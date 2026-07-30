package runtime

import (
	"context"
	"fmt"

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

	decision := agenttools.EvaluateToolPolicy(params.Options, call)
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

	if decision.Action == agenttools.ToolDecisionDeny {
		result := tools.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Status:     tools.CallStatusDenied,
			Error:      decision.Reason,
		}
		_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, "", false
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
	result, err := r.registry.Execute(ctx, call.Name, tc, args)
	if result == nil {
		// MCP fallback: MCP tools aren't in Registry but may still be requested.
		if agenttools.IsMCPToolName(call.Name) {
			mcpResult, mcpErr := r.executeMCPToolRegistry(ctx, call, tc, args)
			return mcpResult, mcpErr.Error(), false
		}
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

	output := ""
	if toolResult.Status == tools.CallStatusFailed && err != nil {
		output = result.Output
	} else if result.Output != "" {
		output = result.Output
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

	if toolResult.Status == tools.CallStatusFailed {
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

// executeMCPToolRegistry executes an MCP tool call through the MCP manager.
// Returns a tools.Result for use in the tool history.
func (r *Runtime) executeMCPToolRegistry(ctx context.Context, call tools.Call, tc *registry.ToolContext, args map[string]any) (tools.Result, error) {
	// MCP tools are dispatched via the MCP manager. For now, return a placeholder
	// since the MCP integration path hasn't been fully migrated.
	return tools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     tools.CallStatusFailed,
		Error:      "MCP tool not yet wired via registry",
	}, nil
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
