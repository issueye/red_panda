package runtime

import (
	"context"
	"fmt"
	"strings"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

func (r *Runtime) executeTool(ctx context.Context, params methods.ReplyParams, invocation agenttools.ToolInvocation) (tools.Result, string, bool) {
	call := invocation.Call
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
	result, output := r.tools.RunWithContext(ctx, agenttools.ToolRunContext{
		WorkingDir:   params.Session.WorkingDir,
		RunID:        params.RunID,
		SessionID:    params.Session.ID,
		AssignmentID: string(assignment.AssignmentID),
		WorkerID:     string(assignment.WorkerID),
		Reply:        &params,
	}, invocation)
	eventCtx := ctx
	if ctx.Err() != nil {
		// Tool cancellation must not suppress the terminal event. WithoutCancel
		// preserves assignment/run values used to build the envelope.
		eventCtx = context.WithoutCancel(ctx)
	}
	if output != "" {
		streamKind := events.StreamToolStdout
		if result.Status == tools.CallStatusFailed {
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
	if result.Status == tools.CallStatusFailed {
		_ = r.emitEvent(eventCtx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, output, false
	}
	_ = r.emitEvent(eventCtx, params, events.EventToolFinished, nil, toolResultPayload(result))
	if call.Name == "todo.write" || call.Name == "todo_write" {
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
	switch call.Name {
	case "goal.create", "goal.plan", "goal.observe", "goal.assess", "goal.finish":
		if state := r.getRunGoal(params.RunID); state != nil && state.Goal.ID != "" {
			_ = r.emitEvent(ctx, params, events.EventGoalUpdated, nil, map[string]any{
				"session_id":   params.Session.ID,
				"run_id":       params.RunID,
				"tool_call_id": call.ID,
				"action":       strings.TrimPrefix(call.Name, "goal."),
				"goal":         state.Goal,
				"iteration":    state.Goal.Iteration,
				"status":       state.Goal.Status,
			})
		}
	}
	return result, output, true
}

// todoStatusCounts 按状态汇总待办项，为工具结果和运行上下文提供一致的统计值。
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

// toolResultPayload 将工具执行结果转换为事件载荷。

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
