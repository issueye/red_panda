package runtime

import (
	"context"
	"fmt"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

func (r *Runtime) executeTool(ctx context.Context, params methods.ReplyParams, invocation ToolInvocation) (tools.Result, string, bool) {
	call := invocation.Call
	decision := EvaluateToolPolicy(params.Options, call)
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

	if decision.Action == ToolDecisionDeny {
		result := tools.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Status:     tools.CallStatusDenied,
			Error:      decision.Reason,
		}
		_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, "", false
	}

	if decision.Action == ToolDecisionRequirePermission {
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

	result, output := r.tools.RunWithContext(ctx, ToolRunContext{
		WorkingDir: params.Session.WorkingDir,
		RunID:      params.RunID,
		SessionID:  params.Session.ID,
		Reply:      &params,
	}, invocation)
	if output != "" {
		streamKind := events.StreamToolStdout
		if result.Status == tools.CallStatusFailed {
			streamKind = events.StreamToolStderr
		}
		_ = r.emitEvent(ctx, params, events.EventToolOutput, &events.StreamRef{
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
	if result.Status == tools.CallStatusFailed {
		_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, output, false
	}
	_ = r.emitEvent(ctx, params, events.EventToolFinished, nil, toolResultPayload(result))
	if call.Name == "todo.write" || call.Name == "todo_write" {
		items := r.getRunTodos(params.RunID)
		open, completed, cancelled := todoStatusCounts(items)
		_ = r.emitEvent(ctx, params, events.EventTodoUpdated, nil, map[string]any{
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
	switch call.Name {
	case "goal.write", "goal.update", "goal.checkpoint", "goal.complete":
		if state := r.getRunGoal(params.RunID); state != nil && state.Goal.ID != "" {
			_ = r.emitEvent(ctx, params, events.EventGoalUpdated, nil, map[string]any{
				"session_id":     params.Session.ID,
				"run_id":         params.RunID,
				"tool_call_id":   call.ID,
				"action":         strings.TrimPrefix(call.Name, "goal."),
				"goal":           state.Goal,
				"pipeline_phase": state.Goal.PipelinePhase,
				"status":         state.Goal.Status,
			})
		}
	}
	return result, output, true
}

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

// callGatewayResult forwards a gateway-backed tool/state RPC and unmarshals the
// typed result. Memory/todo/goal/context all share this path so wire method

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
