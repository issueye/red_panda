package runtime

import (
	"context"

	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type runtimeSubAgent struct {
	record methods.SubAgentRecord
	cancel context.CancelFunc
}

func (r *Runtime) createProcessSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string) (subagent.Process, error) {
	return subagent.NewProcessWithRequestHandler(ctx, params, subAgentID, r.callGateway)
}

// SubagentManager 的实现，作为父代理的工具控制接口。

func (r *Runtime) List(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	return r.executeSubagentList(runCtx, call)
}

func (r *Runtime) Cancel(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	return r.executeSubagentCancel(runCtx, call)
}

func (r *Runtime) Reset(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	return r.executeSubagentReset(runCtx, call)
}

func (r *Runtime) PoolStatus() (string, error) {
	return r.executeSubagentPoolStatus()
}

func (r *Runtime) PoolResize(call tools.Call) (string, error) {
	return r.executeSubagentPoolResize(call)
}

func (r *Runtime) PoolReset() (string, error) {
	return r.executeSubagentPoolReset()
}
