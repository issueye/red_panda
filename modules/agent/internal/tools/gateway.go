package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	return requireGatewayToolCompleted("todo", result.Status, result.Output)
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
	return requireGatewayToolCompleted("goal", result.Status, result.Output)
}

// runContextTool 将 context.*（目标暂存区）工具请求转发到 Gateway。
// context 工具不会被加入子代理拒绝列表，使专业子代理可以读取共享发现并写入交接信息。
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
	output, err := requireGatewayToolCompleted("context", result.Status, result.Output)
	if err != nil {
		return output, err
	}
	// 将结构化笔记加入输出，确保模型能在当前上下文中看到它们。
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
