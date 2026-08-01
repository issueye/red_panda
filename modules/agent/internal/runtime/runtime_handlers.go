package runtime

import (
	"context"
	"encoding/json"
	"time"

	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/skill"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
)

// JSON-RPC request handlers and root run emission (docs/41 W5-5).
func (r *Runtime) handleGatewayResponse(line []byte) error {
	var resp jsonrpc.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil
	}
	r.mu.Lock()
	ch := r.gatewayPending[resp.ID]
	delete(r.gatewayPending, resp.ID)
	r.mu.Unlock()
	if ch != nil {
		ch <- resp
	}
	return nil
}

func (r *Runtime) handleInitialize(req jsonrpc.Request) error {
	var params methods.InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if params.ProtocolVersion != events.ProtocolVersionV2 {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32001, "incompatible protocol version"))
	}
	r.mu.Lock()
	r.initialized = true
	r.protocolVersion = events.ProtocolVersionV2
	r.mu.Unlock()

	// v0.2 capabilities only. Legacy agent.reply / agent.cancel / agent.* subagent methods are gone.
	result := methods.InitializeResult{
		ProtocolVersion: events.ProtocolVersionV2,
		Server:          methods.PeerInfo{Name: "red-panda-agent", Version: r.version},
		Capabilities: []methods.Capability{
			{Name: methods.CorePing, Version: 1},
			{Name: methods.RunExecute, Version: 1},
			{Name: methods.RunCancel, Version: 1},
			{Name: methods.RunPause, Version: 1},
			{Name: methods.RunResume, Version: 1},
			{Name: methods.RunEvent, Version: 1},
			{Name: methods.WorkerList, Version: 1},
			{Name: methods.WorkerAssignmentCancel, Version: 1},
			{Name: methods.WorkerMessageSend, Version: 1},
			{Name: methods.WorkerMessageReceive, Version: 1},
			{Name: methods.WorkerPoolStatus, Version: 1},
			{Name: methods.AgentSkills, Version: 1},
			{Name: methods.AgentSkillLoad, Version: 1},
			{Name: methods.AgentSkillCreate, Version: 1},
			{Name: methods.AgentSkillUpdate, Version: 1},
			{Name: methods.AgentSkillDelete, Version: 1},
			{Name: methods.MCPDiscover, Version: 1},
			{Name: methods.MCPCall, Version: 1},
			{Name: methods.PermissionResolve, Version: 1},
		},
	}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleRunExecute(ctx context.Context, req jsonrpc.Request) error {
	var params methods.RunExecuteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	req.Params = raw
	return r.handleRun(ctx, req)
}

func (r *Runtime) handlePing(req jsonrpc.Request) error {
	var params methods.PingParams
	_ = json.Unmarshal(req.Params, &params)
	resp, err := jsonrpc.NewResult(req.ID, methods.PingResult{Nonce: params.Nonce, Status: "ok"})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleRun(ctx context.Context, req jsonrpc.Request) error {
	var params methods.ReplyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if params.RunID == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "missing run_id"))
	}
	runCtx, cancel := context.WithCancel(ctx)
	if !r.registerRun(params.RunID, cancel) {
		cancel()
		return r.writeResponse(jsonrpc.NewError(req.ID, -32009, "run already exists"))
	}

	executionCtx := withWorkerExecution(runCtx, workerExecutionSpec{Kind: workerExecutionEntry, Params: params, Task: params.Input.Text})
	submitRequest := worker.SubmitRequest{
		RunID: params.RunID,
		Task:  params.Input.Text,
	}
	var assignment worker.AssignmentRef
	var err error
	if proxy := params.Options.WorkerContext; proxy != nil && proxy.ProxyMessages {
		if proxy.RunID != params.RunID || proxy.WorkerID == "" || proxy.AssignmentID == "" {
			r.unregisterRun(params.RunID)
			cancel()
			return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid worker_context"))
		}
		assignment, err = r.workerPool.SubmitRestricted(executionCtx, worker.WorkerID(proxy.WorkerID), submitRequest)
	} else {
		assignment, err = r.workerPool.SubmitEntry(executionCtx, submitRequest)
	}
	if err != nil {
		r.unregisterRun(params.RunID)
		cancel()
		return r.writeResponse(jsonrpc.NewError(req.ID, -32010, err.Error()))
	}

	accepted, err := jsonrpc.NewResult(req.ID, methods.RunExecuteResult{
		Accepted:     true,
		RunID:        params.RunID,
		AssignmentID: string(assignment.AssignmentID),
		WorkerID:     string(assignment.WorkerID),
	})
	if err != nil {
		r.workerPool.Cancel(context.Background(), assignment.AssignmentID, "failed to encode run acceptance")
		cancel()
		go r.unregisterRunWhenSettled(params.RunID, assignment.AssignmentID)
		return err
	}
	if err := r.writeResponse(accepted); err != nil {
		r.workerPool.Cancel(context.Background(), assignment.AssignmentID, "failed to deliver run acceptance")
		cancel()
		go r.unregisterRunWhenSettled(params.RunID, assignment.AssignmentID)
		return err
	}

	go func() {
		r.unregisterRunWhenSettled(params.RunID, assignment.AssignmentID)
	}()
	return nil
}

func (r *Runtime) unregisterRunWhenSettled(runID string, assignmentID worker.AssignmentID) {
	_, _ = r.workerPool.WaitSettled(context.Background(), assignmentID)
	r.unregisterRun(runID)
}

func (r *Runtime) emitRun(ctx context.Context, params methods.ReplyParams) {
	messageID := "msg_" + params.RunID
	streamID := "stream_" + params.RunID + "_message"
	streamSeq := uint64(1)
	hookCtx := runtimeHookContext(ctx, params)
	hookCtx.MessageID = messageID
	runEnd := map[string]any{
		"run_id":        params.RunID,
		"session_id":    params.Session.ID,
		"message_id":    messageID,
		"status":        "failed",
		"provider_name": r.provider.Name(),
	}
	defer func() {
		endCtx := ctx
		if ctx.Err() != nil {
			endCtx = context.WithoutCancel(ctx)
		}
		finalHookCtx := runtimeHookContext(endCtx, params)
		finalHookCtx.MessageID = messageID
		r.bus.Emit(finalHookCtx, hooks.HookRunEnd, runEnd)
	}()

	startOutcome := r.bus.Emit(hookCtx, hooks.HookRunStart, map[string]any{
		"run_id":        params.RunID,
		"session_id":    params.Session.ID,
		"message_id":    messageID,
		"input":         params.Input.Text,
		"provider_name": r.provider.Name(),
	})
	if input, ok := startOutcome.Event["input"].(string); ok {
		params.Input.Text = input
	}
	if startOutcome.Blocked || startOutcome.Cancelled {
		status := "denied"
		if startOutcome.Cancelled {
			status = "cancelled"
		}
		runEnd["status"] = status
		runEnd["reason"] = hookOutcomeReason(startOutcome, "run blocked by hook")
		_ = r.emitEvent(context.WithoutCancel(ctx), params, events.EventFinish, nil, map[string]any{
			"status": status,
			"reason": runEnd["reason"],
		})
		return
	}

	// 每次会话均从磁盘重新加载技能，使新建技能无需重启会话或 Runtime 即可使用。
	params.Options.SkillsContext = skill.BuildContext(params.Session.WorkingDir)
	r.emitSkillsInjected(ctx, params)
	r.emitMemoryInjected(ctx, params)

	if params.Options.RequirePermission {
		decision, ok := r.requestPermission(ctx, params, permission.RequestPayload{
			PermissionID: "perm_" + params.RunID,
			RunID:        params.RunID,
			ToolCallID:   "checkpoint_" + params.RunID,
			ToolName:     "runtime.continue",
			Risk:         "medium",
			Summary:      "Allow Runtime MVP to continue past the protected checkpoint.",
			Detail:       "This validates the desktop, gateway, and Agent Runtime permission flow.",
			Arguments: map[string]any{
				"text": params.Input.Text,
			},
		})
		if !ok {
			if ctx.Err() != nil {
				runEnd["status"] = "cancelled"
				r.emitCancelled(params)
				return
			}
			runEnd["reason"] = "permission request was not resolved"
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message": "permission request was not resolved",
				"status":  "failed",
			})
			return
		}
		if decision.Decision != permission.DecisionApprove {
			runEnd["status"] = "denied"
			runEnd["reason"] = "permission denied"
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message":       "permission denied",
				"permission_id": decision.PermissionID,
				"status":        "denied",
			})
			_ = r.emitEvent(ctx, params, events.EventFinish, nil, map[string]any{
				"status": "denied",
			})
			return
		}
		_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
			StreamID: streamID,
			Kind:     events.StreamMessage,
			Seq:      streamSeq,
			Final:    false,
		}, map[string]any{
			"message_id": messageID,
			"delta":      "permission approved\n",
		})
		streamSeq++
	}

	providerInput := params.Input.Text
	var toolHistory []provider.ToolExchange

	seg := r.runProviderLoopSegment(ctx, params, providerInput, toolHistory, messageID, streamID, &streamSeq)
	// 仅在根运行结束时清理快照，不能在分段中途清理。
	r.clearRunSnapshots(params.RunID)

	status := finishStatusFromLoopEnd(seg.Reason)
	runEnd["status"] = status
	runEnd["loop_end_reason"] = string(seg.Reason)
	runEnd["tool_turns"] = seg.ToolTurns
	for key, value := range providerStatsPayload(seg.Stats) {
		runEnd[key] = value
	}
	if status != "completed" {
		if status == "cancelled" {
			r.emitCancelled(params)
		} else {
			finishPayload := map[string]any{
				"status":          status,
				"loop_end_reason": string(seg.Reason),
			}
			for key, value := range providerStatsPayload(seg.Stats) {
				finishPayload[key] = value
			}
			_ = r.emitEvent(ctx, params, events.EventFinish, nil, finishPayload)
		}
		return
	}

	if ctx.Err() != nil {
		runEnd["status"] = "cancelled"
		r.emitCancelled(params)
		return
	}
	finishPayload := map[string]any{
		"status":          "completed",
		"provider_name":   r.provider.Name(),
		"loop_end_reason": string(seg.Reason),
		"tool_turns":      seg.ToolTurns,
	}
	for key, value := range providerStatsPayload(seg.Stats) {
		finishPayload[key] = value
	}
	if seg.Reason == loopEndMaxTurns {
		finishPayload["max_turns_reached"] = true
	}
	if seg.Reason == loopEndToolBudget {
		finishPayload["tool_budget_reached"] = true
	}
	_ = r.emitEvent(ctx, params, events.EventFinish, nil, finishPayload)
}

func providerStatsPayload(stats providerExecutionStats) map[string]any {
	return map[string]any{
		"max_turns":            stats.MaxTurns,
		"loop_turns":           stats.LoopTurns,
		"provider_requests":    stats.ProviderRequests,
		"tool_calls_requested": stats.ToolCallsRequested,
		"tool_calls_executed":  stats.ToolCallsExecuted,
		"tool_call_budget":     stats.ToolCallBudget,
		"max_turns_reached":    stats.MaxTurnsReached,
		"tool_budget_reached":  stats.ToolBudgetReached,
	}
}

func (r *Runtime) emitCancelled(params methods.ReplyParams) {
	_ = r.emitEvent(context.Background(), params, events.EventFinish, nil, map[string]any{
		"status": "cancelled",
	})
}

// clearRunSnapshots 清理运行快照中的 per-run 缓存上下文（如 Todo）。
// 仅在根运行结束时调用，避免在分段循环中途清理。
func (r *Runtime) clearRunSnapshots(runID string) {
	r.runStates.ClearSnapshots(runID)
}

func (r *Runtime) requestPermission(ctx context.Context, params methods.ReplyParams, payload permission.RequestPayload) (permission.ResolveParams, bool) {
	assignment := assignmentFromContext(ctx)
	if r.workerPool != nil && assignment.AssignmentID != "" {
		if err := r.workerPool.SetWaitingPermission(assignment.AssignmentID, true); err == nil {
			defer func() { _ = r.workerPool.SetWaitingPermission(assignment.AssignmentID, false) }()
		}
	}
	if payload.PermissionID == "" {
		payload.PermissionID = "perm_" + params.RunID
	}
	if payload.RunID == "" {
		payload.RunID = params.RunID
	}
	ch := make(chan permission.ResolveParams, 1)

	r.mu.Lock()
	r.permissions[payload.PermissionID] = ch
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.permissions, payload.PermissionID)
		r.mu.Unlock()
	}()

	_ = r.emitEvent(ctx, params, events.EventPermissionRequest, nil, map[string]any{
		"permission_id": payload.PermissionID,
		"run_id":        payload.RunID,
		"tool_call_id":  payload.ToolCallID,
		"tool_name":     payload.ToolName,
		"risk":          payload.Risk,
		"summary":       payload.Summary,
		"detail":        payload.Detail,
		"arguments":     payload.Arguments,
	})

	select {
	case <-ctx.Done():
		return permission.ResolveParams{}, false
	case decision := <-ch:
		return decision, true
	case <-time.After(5 * time.Minute):
		return permission.ResolveParams{}, false
	}
}

func (r *Runtime) handlePermissionResolve(req jsonrpc.Request) error {
	var params permission.ResolveParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if params.PermissionID == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "missing permission_id"))
	}

	r.mu.Lock()
	ch := r.permissions[params.PermissionID]
	r.mu.Unlock()
	if ch == nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32004, "permission request not found"))
	}
	ch <- params

	resp, err := jsonrpc.NewResult(req.ID, permission.ResolveResult{Accepted: true})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleCancel(req jsonrpc.Request) error {
	var params methods.CancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if params.RunID == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "missing run_id"))
	}
	cancelled := r.cancelRun(params.RunID)
	resp, err := jsonrpc.NewResult(req.ID, map[string]any{
		"accepted":  true,
		"run_id":    params.RunID,
		"cancelled": cancelled,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}
