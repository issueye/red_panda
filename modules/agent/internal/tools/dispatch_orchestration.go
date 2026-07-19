package tools

import (
	"context"
	"fmt"

	"redpanda/agent/internal/skill"
	ptools "redpanda/protocol/tools"
)

// dispatchOrchestrationTool handles skill.* and worker.* tools.
func (runner ToolRunner) dispatchOrchestrationTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (output string, handled bool, err error) {
	switch call.Name {
	case "skill.list":
		out, e := skill.RunList(runCtx.WorkingDir)
		return out, true, e
	case "skill.create":
		out, e := skill.RunCreate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
		return out, true, e
	case "skill.update":
		out, e := skill.RunUpdate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
		return out, true, e
	case "skill.delete":
		out, e := skill.RunDelete(runCtx.WorkingDir, StringArg(call.Arguments, "name"))
		return out, true, e
	case "skill.run":
		if runner.SkillExecutor == nil {
			return "", true, fmt.Errorf("skill Worker executor is not available")
		}
		out, e := runner.SkillExecutor(ctx, runCtx, call)
		return out, true, e
	case "worker.delegate":
		if runner.WorkerDelegate == nil {
			return "", true, fmt.Errorf("worker delegate executor is not available")
		}
		out, e := runner.WorkerDelegate(ctx, runCtx, call)
		return out, true, e
	case "worker.list":
		if runner.WorkerList == nil {
			return "", true, fmt.Errorf("worker list executor is not available")
		}
		out, e := runner.WorkerList(ctx, runCtx, call)
		return out, true, e
	case "worker.cancel":
		if runner.WorkerCancel == nil {
			return "", true, fmt.Errorf("worker cancel executor is not available")
		}
		out, e := runner.WorkerCancel(ctx, runCtx, call)
		return out, true, e
	case "worker.pool_status":
		if runner.WorkerPoolStatus == nil {
			return "", true, fmt.Errorf("worker pool status executor is not available")
		}
		out, e := runner.WorkerPoolStatus(ctx, runCtx, call)
		return out, true, e
	case "worker.send":
		if runner.WorkerSend == nil {
			return "", true, fmt.Errorf("worker send executor is not available")
		}
		out, e := runner.WorkerSend(ctx, runCtx, call)
		return out, true, e
	case "worker.receive":
		if runner.WorkerReceive == nil {
			return "", true, fmt.Errorf("worker receive executor is not available")
		}
		out, e := runner.WorkerReceive(ctx, runCtx, call)
		return out, true, e
	default:
		return "", false, nil
	}
}
