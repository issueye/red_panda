package runtime

import (
	"context"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type runtimeSubAgent struct {
	record methods.SubAgentRecord
	cancel context.CancelFunc
}

type processSubAgent interface {
	Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error
	Cancel(ctx context.Context, runID string, reason string) error
	Close(ctx context.Context) error
}

func (r *Runtime) createProcessSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
	return newSubAgentProcessWithRequestHandler(ctx, params, subAgentID, r.callGateway)
}

// SubagentManager implementation (parent-agent tool control surface).

func (r *Runtime) List(runCtx ToolRunContext, call tools.Call) (string, error) {
	return r.executeSubagentList(runCtx, call)
}

func (r *Runtime) Cancel(runCtx ToolRunContext, call tools.Call) (string, error) {
	return r.executeSubagentCancel(runCtx, call)
}

func (r *Runtime) Reset(runCtx ToolRunContext, call tools.Call) (string, error) {
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
