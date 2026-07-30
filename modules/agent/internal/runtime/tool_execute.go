package runtime

import (
	"context"
	"fmt"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

func (r *Runtime) executeTool(ctx context.Context, params methods.ReplyParams, call tools.Call) (tools.Result, string, bool) {
	// Normalize legacy aliases before policy, events, and dispatch.
	call.Name = agenttools.CanonicalToolName(call.Name)

	runRegistry := r.registryForRun(params.RunID)
	entry, registered := runRegistry.Lookup(call.Name)
	hookCtx := runtimeHookContext(ctx, params)
	callOutcome := r.bus.Emit(hookCtx, hooks.HookToolCall, map[string]any{
		"tool_call_id": call.ID,
		"tool_name":    call.Name,
		"display_name": call.DisplayName,
		"risk":         string(call.Risk),
		"arguments":    call.Arguments,
	})
	if callOutcome.Blocked || callOutcome.Cancelled {
		return r.denyToolCall(ctx, params, call, hookOutcomeReason(callOutcome, "tool call blocked"))
	}
	if transformed, exists := callOutcome.Event["arguments"]; exists {
		arguments, ok := transformed.(map[string]any)
		if !ok && transformed != nil {
			return r.denyToolCall(ctx, params, call, "tool.call hook produced invalid arguments")
		}
		call.Arguments = arguments
	}

	opsOnly := registered && entry.OpsOnly
	decision := agenttools.EvaluateToolPolicy(params.Options, call, opsOnly)
	permissionOutcome := r.bus.Emit(hookCtx, hooks.HookPermissionCheck, map[string]any{
		"tool_call_id": call.ID,
		"tool_name":    call.Name,
		"display_name": call.DisplayName,
		"risk":         string(call.Risk),
		"arguments":    call.Arguments,
		"action":       string(decision.Action),
		"reason":       decision.Reason,
	})
	if permissionOutcome.Blocked || permissionOutcome.Cancelled {
		r.emitToolStarted(ctx, params, call, decision)
		return r.denyToolCall(ctx, params, call, hookOutcomeReason(permissionOutcome, "permission check blocked tool"))
	}
	decision = toolDecisionFromHook(permissionOutcome, decision)
	r.emitToolStarted(ctx, params, call, decision)

	if decision.Action == agenttools.ToolDecisionDeny {
		return r.denyToolCall(ctx, params, call, decision.Reason)
	}
	if decision.Action != agenttools.ToolDecisionAllow && decision.Action != agenttools.ToolDecisionRequirePermission {
		return r.denyToolCall(ctx, params, call, fmt.Sprintf("invalid permission action %q", decision.Action))
	}

	if decision.Action == agenttools.ToolDecisionRequirePermission {
		decision, ok := r.requestPermission(ctx, params, permission.RequestPayload{
			PermissionID: "perm_" + call.ID,
			RunID:        params.RunID,
			ToolCallID:   call.ID,
			ToolName:     call.Name,
			Risk:         string(call.Risk),
			Summary:      fmt.Sprintf("Allow %s to run.", call.DisplayName),
			Detail:       "The Agent Runtime is waiting for permission before executing this tool.",
			Arguments:    call.Arguments,
		})
		if !ok || decision.Decision != permission.DecisionApprove {
			result := tools.Result{
				ToolCallID: call.ID,
				Name:       call.Name,
				Status:     tools.CallStatusDenied,
				Error:      "permission denied",
			}
			_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
			return result, "", false
		}
	}

	assignment := assignmentFromContext(ctx)
	tc := &registry.ToolContext{
		ToolCallID:   call.ID,
		RunID:        params.RunID,
		SessionID:    params.Session.ID,
		WorkingDir:   params.Session.WorkingDir,
		AssignmentID: string(assignment.AssignmentID),
		WorkerID:     string(assignment.WorkerID),
		Reply:        &params,
	}

	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}

	// Registry dispatch (v0.3.0)
	result, err := runRegistry.Execute(ctx, call.Name, tc, args)
	if result == nil {
		return tools.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Status:     tools.CallStatusFailed,
			Error:      fmt.Sprintf("unknown tool %s", call.Name),
		}, "", false
	}

	// Wrap result into legacy tools.Result format
	toolResult := tools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     tools.CallStatus(result.Status),
		ExitCode:   result.ExitCode,
		Output:     result.Output,
		Error:      result.Error,
		DurationMS: result.DurationMS,
	}
	if err != nil {
		toolResult.Status = tools.CallStatusFailed
		if toolResult.Error == "" {
			toolResult.Error = err.Error()
		}
	}
	toolResult = toolResultFromHook(r.bus.Emit(hookCtx, hooks.HookToolResult, toolResultPayload(toolResult)), toolResult)

	output := ""
	if toolResult.Output != "" {
		output = toolResult.Output
	}

	eventCtx := ctx
	if ctx.Err() != nil {
		eventCtx = context.WithoutCancel(ctx)
	}
	if output != "" {
		streamKind := events.StreamToolStdout
		if toolResult.Status == tools.CallStatusFailed {
			streamKind = events.StreamToolStderr
		}
		_ = r.emitEvent(eventCtx, params, events.EventToolOutput, &events.StreamRef{
			StreamID: "stream_" + call.ID,
			Kind:     streamKind,
			Seq:      1,
			Final:    true,
		}, map[string]any{
			"tool_call_id": call.ID,
			"tool_name":    call.Name,
			"delta":        output,
		})
	}

	if toolResult.Status != tools.CallStatusCompleted {
		_ = r.emitEvent(eventCtx, params, events.EventToolFailed, nil, toolResultPayload(toolResult))
		return toolResult, output, false
	}
	_ = r.emitEvent(eventCtx, params, events.EventToolFinished, nil, toolResultPayload(toolResult))
	if methods.IsTodoWriteTool(call.Name) {
		items := r.getRunTodos(params.RunID)
		open, completed, cancelled := todoStatusCounts(items)
		_ = r.emitEvent(eventCtx, params, events.EventTodoUpdated, nil, map[string]any{
			"session_id":      params.Session.ID,
			"run_id":          params.RunID,
			"tool_call_id":    call.ID,
			"action":          "write",
			"items":           items,
			"open_count":      open,
			"completed_count": completed,
			"cancelled_count": cancelled,
		})
	}
	return toolResult, output, true
}

func (r *Runtime) emitToolStarted(ctx context.Context, params methods.ReplyParams, call tools.Call, decision agenttools.ToolDecision) {
	_ = r.emitEvent(ctx, params, events.EventToolStarted, nil, map[string]any{
		"tool_call_id":  call.ID,
		"tool_name":     call.Name,
		"display_name":  call.DisplayName,
		"risk":          string(call.Risk),
		"arguments":     call.Arguments,
		"status":        string(tools.CallStatusRunning),
		"policy":        string(decision.Action),
		"policy_reason": decision.Reason,
	})
}

func runtimeHookContext(ctx context.Context, params methods.ReplyParams) *hooks.HookContext {
	return &hooks.HookContext{
		Context:   ctx,
		RunID:     params.RunID,
		SessionID: params.Session.ID,
		CWD:       params.Session.WorkingDir,
	}
}

func hookOutcomeReason(outcome hooks.HookOutcome, fallback string) string {
	if outcome.Reason != "" {
		return outcome.Reason
	}
	return fallback
}

func toolDecisionFromHook(outcome hooks.HookOutcome, fallback agenttools.ToolDecision) agenttools.ToolDecision {
	decision := fallback
	if action, ok := outcome.Event["action"].(string); ok {
		decision.Action = agenttools.ToolDecisionAction(action)
	}
	if reason, ok := outcome.Event["reason"].(string); ok {
		decision.Reason = reason
	}
	return decision
}

func toolResultFromHook(outcome hooks.HookOutcome, fallback tools.Result) tools.Result {
	if outcome.Blocked || outcome.Cancelled {
		fallback.Status = tools.CallStatusFailed
		fallback.Error = hookOutcomeReason(outcome, "tool result blocked")
		return fallback
	}
	result := fallback
	if status, ok := outcome.Event["status"].(string); ok {
		result.Status = tools.CallStatus(status)
	}
	if output, ok := outcome.Event["output"].(string); ok {
		result.Output = output
	}
	if message, ok := outcome.Event["error"].(string); ok {
		result.Error = message
	}
	if exitCode, ok := hookInt(outcome.Event["exit_code"]); ok {
		result.ExitCode = exitCode
	}
	if durationMS, ok := hookInt64(outcome.Event["duration_ms"]); ok {
		result.DurationMS = durationMS
	}
	switch result.Status {
	case tools.CallStatusCompleted, tools.CallStatusFailed, tools.CallStatusDenied:
	default:
		result.Status = tools.CallStatusFailed
		result.Error = "tool.result hook produced invalid status"
	}
	return result
}

func hookInt(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case float64:
		return int(number), true
	default:
		return 0, false
	}
}

func hookInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case float64:
		return int64(number), true
	default:
		return 0, false
	}
}

func (r *Runtime) denyToolCall(ctx context.Context, params methods.ReplyParams, call tools.Call, reason string) (tools.Result, string, bool) {
	result := tools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     tools.CallStatusDenied,
		Error:      reason,
	}
	_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
	return result, "", false
}

// todoStatusCounts tallies TODO items by status for the todo.statusUpdated event.
func todoStatusCounts(items []methods.TodoItemDTO) (open, completed, cancelled int) {
	for _, item := range items {
		switch item.Status {
		case "pending", "in_progress":
			open++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}
	return
}

// toolResultPayload converts a tools.Result into the event payload map used
// by the runtime event bus.
func toolResultPayload(result tools.Result) map[string]any {
	return map[string]any{
		"tool_call_id": result.ToolCallID,
		"tool_name":    result.Name,
		"status":       string(result.Status),
		"exit_code":    result.ExitCode,
		"output":       result.Output,
		"error":        result.Error,
		"duration_ms":  result.DurationMS,
	}
}
