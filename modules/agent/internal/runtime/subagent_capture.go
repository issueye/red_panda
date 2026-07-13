package runtime

import (
	"context"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

func (r *Runtime) failWorkerSubAgent(params methods.ReplyParams, subAgentID string, agentName string, backend string, err error) {
	r.finishSubAgent(params.RunID, subAgentID, "failed", "subagent failed", err.Error())
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"status":      "failed",
		"summary":     "subagent failed",
		"backend":     backend,
		"error":       err.Error(),
	})
}
