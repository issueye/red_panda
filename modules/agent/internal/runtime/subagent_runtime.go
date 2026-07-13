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
	r.subagents.Register(subagent.Registration{
		SubAgentID:      subAgentID,
		Name:            "planner",
		Backend:         backend,
		RootRunID:       params.RunID,
		ParentRunID:     params.RunID,
		ParentSessionID: params.Session.ID,
		ChildRunID:      params.RunID + ":subagent:" + subAgentID,
		Summary:         "planner subagent started",
		Cancel:          cancel,
	})
	go func() {
		defer close(done)
		if backend == "runtime_process" || backend == "process_pool" {
			r.runProcessPlannerSubAgent(ctx, params, subAgentID, backend)
			return
		}
		defer r.subagents.Finish(params.RunID, subAgentID, "completed", "planner subagent completed", "")
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
	child, release, err := (runtimeProcessProvider{runtime: r}).Acquire(ctx, params, subAgentID, backend)
	if err != nil {
		r.subagents.Finish(params.RunID, subAgentID, "failed", backend+" planner subagent failed", err.Error())
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

	bridgeSpec := subagent.RunSpec{
		RootRunID:  params.RunID,
		SubAgentID: subAgentID,
		Name:       "planner",
		Backend:    backend,
		Parent:     params,
		Child:      childParams,
	}
	err = child.Start(ctx, childParams, func(event events.Envelope) {
		_ = (runtimeEventSink{runtime: r}).Bridge(context.Background(), bridgeSpec, event)
	})
	if err != nil {
		if ctx.Err() != nil {
			r.markSubAgentCancelledWithBackend(params, subAgentID, backend)
			return
		}
		r.subagents.Finish(params.RunID, subAgentID, "failed", backend+" planner subagent failed", err.Error())
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
	r.subagents.Finish(params.RunID, subAgentID, "completed", backend+" planner subagent completed", "")
	_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "completed",
		"summary":     backend + " planner subagent completed",
		"backend":     backend,
	})
}

func (r *Runtime) markSubAgentCancelled(params methods.ReplyParams, subAgentID string) {
	r.markSubAgentCancelledWithBackend(params, subAgentID, "in_process")
}

func (r *Runtime) markSubAgentCancelledWithBackend(params methods.ReplyParams, subAgentID string, backend string) {
	r.subagents.Finish(params.RunID, subAgentID, "cancelled", "planner subagent cancelled", "")
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
	resp, err := jsonrpc.NewResult(req.ID, methods.SubAgentsResult{Items: r.subagents.List(params)})
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
	cancelled := r.subagents.Cancel(params.RunID, params.SubAgentID)
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
