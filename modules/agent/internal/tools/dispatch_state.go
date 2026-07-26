package tools

import (
	"context"

	ptools "redpanda/protocol/tools"
)

// dispatchStateTool handles Gateway-backed todo/memory tools.
func (runner ToolRunner) dispatchStateTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (output string, handled bool, err error) {
	switch call.Name {
	case "todo.write", "todo.list":
		out, e := runner.runTodoTool(ctx, runCtx, call)
		return out, true, e
	case "memory.list", "memory.create", "memory.update", "memory.delete":
		out, e := runner.runMemoryTool(ctx, runCtx, call)
		return out, true, e
	default:
		return "", false, nil
	}
}
