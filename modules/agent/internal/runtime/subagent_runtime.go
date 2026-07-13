package runtime

import (
	"context"
	"encoding/json"
	"time"

	"redpanda/agent/internal/subagent"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func (r *Runtime) startPlannerSubAgent(parentCtx context.Context, params methods.ReplyParams) <-chan struct{} {
	done := make(chan struct{})
	subAgentID := "planner_" + params.RunID
	ctx, cancel := context.WithCancel(parentCtx)
	backend := normalizedSubAgentBackend(params.Options.SubAgentBackend)
	r.registerSubAgent(params, subAgentID, "planner", backend, cancel)
	go func() {
		defer close(done)
		if backend == "runtime_process" || backend == "process_pool" {
			r.runProcessPlannerSubAgent(ctx, params, subAgentID, backend)
			return
		}
		defer r.finishSubAgent(params.RunID, subAgentID, "completed", "planner subagent completed", "")
		r.emitPlannerSubAgent(ctx, params, subAgentID, backend)
	}()
	return done
}

func (r *Runtime) emitPlannerSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) {
	agent := subAgentRef(subAgentID, "planner")

	_ = r.emitAgentEvent(ctx, params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "running",
		"summary":     "planner subagent started",
		"backend":     backend,
	})
	if !sleepContext(ctx, plannerStartDelay) {
		r.markSubAgentCancelled(params, subAgentID)
		return
	}

	streamID := "stream_" + params.RunID + "_planner"
	_ = r.emitAgentEvent(ctx, params, agent, events.EventReasoningDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamReasoning,
		Seq:      1,
		Final:    false,
	}, map[string]any{
		"delta": "planner: analyzing task\n",
	})
	if !sleepContext(ctx, plannerDraftDelay) {
		r.markSubAgentCancelled(params, subAgentID)
		return
	}

	_ = r.emitAgentEvent(ctx, params, agent, events.EventMessageDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamMessage,
		Seq:      2,
		Final:    true,
	}, map[string]any{
		"delta": "planner: draft plan ready",
	})
	_ = r.emitAgentEvent(ctx, params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "completed",
		"summary":     "planner subagent completed",
		"backend":     backend,
	})
}

func (r *Runtime) runProcessPlannerSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) {
	agent := subAgentRef(subAgentID, "planner")
	_ = r.emitAgentEvent(ctx, params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "running",
		"summary":     backend + " planner subagent started",
		"backend":     backend,
	})
	child, release, err := r.acquireProcessSubAgent(ctx, params, subAgentID, backend)
	if err != nil {
		r.finishSubAgent(params.RunID, subAgentID, "failed", backend+" planner subagent failed", err.Error())
		_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
			"subagent_id": subAgentID,
			"name":        "planner",
			"status":      "failed",
			"summary":     backend + " planner subagent failed",
			"backend":     backend,
			"error":       err.Error(),
		})
		return
	}
	reusable := false
	defer func() {
		if release != nil {
			release(reusable)
		}
	}()

	childRunID := params.RunID + ":subagent:" + subAgentID
	childParams := params
	childParams.RunID = childRunID
	childParams.Input.Text = "planner subagent task: " + params.Input.Text
	childParams.Options.SpawnSubAgents = false
	childParams.Options.SubAgentBackend = ""
	disableGoalPipelineForChild(&childParams.Options)

	err = child.Start(ctx, childParams, func(event events.Envelope) {
		r.bridgeProcessSubAgentEvent(context.Background(), params, subAgentID, "planner", backend, event)
	})
	if err != nil {
		if ctx.Err() != nil {
			r.markSubAgentCancelledWithBackend(params, subAgentID, backend)
			return
		}
		r.finishSubAgent(params.RunID, subAgentID, "failed", backend+" planner subagent failed", err.Error())
		_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
			"subagent_id": subAgentID,
			"name":        "planner",
			"status":      "failed",
			"summary":     backend + " planner subagent failed",
			"backend":     backend,
			"error":       err.Error(),
		})
		return
	}
	if ctx.Err() != nil {
		r.markSubAgentCancelledWithBackend(params, subAgentID, backend)
		return
	}
	reusable = true
	r.finishSubAgent(params.RunID, subAgentID, "completed", backend+" planner subagent completed", "")
	_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "completed",
		"summary":     backend + " planner subagent completed",
		"backend":     backend,
	})
}

func (r *Runtime) acquireProcessSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) (subagent.Process, func(bool), error) {
	if backend == "process_pool" {
		return r.processPool.Acquire(ctx, params, subAgentID)
	}
	child, err := r.newProcessSubAgent(ctx, params, subAgentID)
	if err != nil {
		return nil, nil, err
	}
	return child, func(reusable bool) {
		_ = child.Close(context.Background())
	}, nil
}

func (r *Runtime) bridgeProcessSubAgentEvent(ctx context.Context, params methods.ReplyParams, subAgentID string, agentName string, backend string, child events.Envelope) {
	if child.Type == events.EventFinish {
		return
	}
	payload := copyPayload(child.Payload)
	payload["subagent_id"] = subAgentID
	payload["backend"] = backend
	if child.Type == events.EventSubAgentUpdate {
		payload["name"] = firstPayloadString(payload, "name", agentName)
	}
	stream := child.Stream
	if stream != nil {
		next := *stream
		next.StreamID = "stream_" + subAgentID + "_" + stream.StreamID
		stream = &next
	}
	_ = r.emitAgentEvent(ctx, params, subAgentRef(subAgentID, agentName), child.Type, stream, payload)
}

func (r *Runtime) registerSubAgent(params methods.ReplyParams, subAgentID string, name string, backend string, cancel context.CancelFunc) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := methods.SubAgentRecord{
		SubAgentID:      subAgentID,
		Name:            name,
		Backend:         backend,
		Status:          "running",
		RootRunID:       params.RunID,
		ParentRunID:     params.RunID,
		ParentSessionID: params.Session.ID,
		ChildRunID:      params.RunID + ":subagent:" + subAgentID,
		Summary:         name + " subagent started",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.mu.Lock()
	r.subagents[subAgentID] = &runtimeSubAgent{record: record, cancel: cancel}
	r.mu.Unlock()
}

func (r *Runtime) finishSubAgent(rootRunID string, subAgentID string, status string, summary string, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.subagents[subAgentID]
	if state == nil {
		return
	}
	if state.record.RootRunID != rootRunID {
		return
	}
	switch state.record.Status {
	case "cancelled", "failed", "completed":
		return
	}
	state.record.Status = status
	state.record.Summary = summary
	state.record.Error = errText
	state.record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	state.cancel = nil
}

func (r *Runtime) markSubAgentCancelled(params methods.ReplyParams, subAgentID string) {
	r.markSubAgentCancelledWithBackend(params, subAgentID, "in_process")
}

func (r *Runtime) markSubAgentCancelledWithBackend(params methods.ReplyParams, subAgentID string, backend string) {
	r.finishSubAgent(params.RunID, subAgentID, "cancelled", "planner subagent cancelled", "")
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, "planner"), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "cancelled",
		"summary":     "planner subagent cancelled",
		"backend":     backend,
	})
}

func (r *Runtime) handleSubAgents(req jsonrpc.Request) error {
	var params methods.SubAgentsParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
		}
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.SubAgentsResult{Items: r.subAgentRecords(params)})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleSubAgentCancel(req jsonrpc.Request) error {
	var params methods.SubAgentCancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if params.RunID == "" || params.SubAgentID == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "missing run_id or subagent_id"))
	}
	cancelled := r.cancelSubAgent(params.RunID, params.SubAgentID)
	resp, err := jsonrpc.NewResult(req.ID, methods.SubAgentCancelResult{
		Accepted:   true,
		RunID:      params.RunID,
		SubAgentID: params.SubAgentID,
		Cancelled:  cancelled,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) subAgentRecords(params methods.SubAgentsParams) []methods.SubAgentRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]methods.SubAgentRecord, 0, len(r.subagents))
	for _, state := range r.subagents {
		if params.RunID != "" && state.record.RootRunID != params.RunID {
			continue
		}
		if params.SubAgentID != "" && state.record.SubAgentID != params.SubAgentID {
			continue
		}
		items = append(items, state.record)
	}
	return items
}

func (r *Runtime) cancelSubAgent(rootRunID string, subAgentID string) bool {
	r.mu.Lock()
	state := r.subagents[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID || state.cancel == nil {
		r.mu.Unlock()
		return false
	}
	cancel := state.cancel
	r.mu.Unlock()
	cancel()
	return true
}

func subAgentRef(subAgentID string, name string) events.AgentRef {
	return events.AgentRef{
		AgentID:       subAgentID,
		Role:          events.AgentRoleSubAgent,
		SubAgentID:    subAgentID,
		ParentAgentID: "root",
		Path:          []string{"root", subAgentID},
		Name:          name,
	}
}

func normalizedSubAgentBackend(value string) string {
	switch value {
	case "runtime_process":
		return "runtime_process"
	case "process_pool":
		return "process_pool"
	default:
		return "in_process"
	}
}

func copyPayload(payload map[string]any) map[string]any {
	next := map[string]any{}
	for key, value := range payload {
		next[key] = value
	}
	return next
}

func firstPayloadString(payload map[string]any, key string, fallback string) string {
	if value, ok := payload[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
