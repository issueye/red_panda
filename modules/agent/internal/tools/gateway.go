package tools

import (
	"context"
	"fmt"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

// requireGatewayToolCompleted maps Gateway status into a tool error while still
// returning the output body for the model (shared by all state domains).
func requireGatewayToolCompleted(domain, status, output string) (string, error) {
	if status != "" && status != "completed" {
		return output, fmt.Errorf("%s tool returned status %s", domain, status)
	}
	return output, nil
}

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
	return requireGatewayToolCompleted("memory", result.Status, result.Output)
}

func (runner ToolRunner) runTodoTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if runner.TodoExecutor == nil {
		return "", fmt.Errorf("todo tool executor is not available")
	}
	result, err := runner.TodoExecutor(ctx, methods.TodoToolExecuteParams{
		RunID:         runCtx.RunID,
		SessionID:     runCtx.SessionID,
		WorkspaceRoot: runCtx.WorkingDir,
		ToolCallID:    call.ID,
		ToolName:      CanonicalToolName(call.Name),
		Arguments:     call.Arguments,
	})
	if err != nil {
		return "", err
	}
	return requireGatewayToolCompleted("todo", result.Status, result.Output)
}
