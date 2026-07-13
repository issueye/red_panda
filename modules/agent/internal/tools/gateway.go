package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

func (runner ToolRunner) runMemoryTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if runner.MemoryExecutor == nil {
		return "", fmt.Errorf("memory tool executor is not available")
	}
	result, err := runner.MemoryExecutor(ctx, methods.MemoryToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	if result.Status != "" && result.Status != "completed" {
		return result.Output, fmt.Errorf("memory tool returned status %s", result.Status)
	}
	return result.Output, nil
}

func (runner ToolRunner) runTodoTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if runner.TodoExecutor == nil {
		return "", fmt.Errorf("todo tool executor is not available")
	}
	name := call.Name
	if name == "todo_write" {
		name = "todo.write"
	}
	result, err := runner.TodoExecutor(ctx, methods.TodoToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      name,
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	if result.Status != "" && result.Status != "completed" {
		return result.Output, fmt.Errorf("todo tool returned status %s", result.Status)
	}
	return result.Output, nil
}

func (runner ToolRunner) runGoalTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if runner.GoalExecutor == nil {
		return "", fmt.Errorf("goal tool executor is not available")
	}
	result, err := runner.GoalExecutor(ctx, methods.GoalToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	if result.Status != "" && result.Status != "completed" {
		return result.Output, fmt.Errorf("goal tool returned status %s", result.Status)
	}
	return result.Output, nil
}

// runContextTool dispatches a context.* (goal scratchpad) tool to the Gateway.
// Unlike goal/todo tools, context tools are intentionally NOT on the subagent
// denylist, so specialist children can read shared findings and write handoffs.
func (runner ToolRunner) runContextTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if runner.ContextExecutor == nil {
		return "", fmt.Errorf("context tool executor is not available")
	}
	result, err := runner.ContextExecutor(ctx, methods.ContextToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	if result.Status != "" && result.Status != "completed" {
		return result.Output, fmt.Errorf("context tool returned status %s", result.Status)
	}
	// Include structured notes in the output so the model sees them inline.
	output := result.Output
	if len(result.Notes) > 0 {
		notesJSON, _ := json.Marshal(map[string]any{"notes": result.Notes})
		if output == "" {
			output = string(notesJSON)
		} else {
			output = strings.TrimSpace(output) + "\n" + string(notesJSON)
		}
	}
	return TruncateToolOutput(output), nil
}
