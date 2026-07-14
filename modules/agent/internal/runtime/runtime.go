package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"redpanda/agent/internal/skill"
	"strconv"
	"strings"
	"sync"
	"time"

	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/agent/internal/provider"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
)

const (
	defaultProviderToolTurns = 12
	maxProviderToolTurnsCap  = 48
	maxJSONRPCLineBytes      = 4 * 1024 * 1024
)

var plannerStartDelay = durationFromEnvMillis("RED_PANDA_PLANNER_START_DELAY_MS", 10*time.Millisecond)
var plannerDraftDelay = durationFromEnvMillis("RED_PANDA_PLANNER_DRAFT_DELAY_MS", 120*time.Millisecond)

// effectiveProviderToolTurns 返回单次回复中提供方与工具循环的预算。
func effectiveProviderToolTurns(options methods.ReplyOptions) int {
	if options.MaxToolTurns > 0 {
		if options.MaxToolTurns > maxProviderToolTurnsCap {
			return maxProviderToolTurnsCap
		}
		return options.MaxToolTurns
	}
	if env := os.Getenv("RED_PANDA_MAX_TOOL_TURNS"); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			if parsed > maxProviderToolTurnsCap {
				return maxProviderToolTurnsCap
			}
			return parsed
		}
	}
	return defaultProviderToolTurns
}

type Runtime struct {
	in      io.Reader
	out     io.Writer
	log     io.Writer
	version string

	mu              sync.Mutex
	eventMu         sync.Mutex
	initialized     bool
	protocolVersion string
	nextSeq         map[string]uint64
	agentSeq        map[string]map[string]uint64
	gatewayPending  map[jsonrpc.ID]chan jsonrpc.Response
	nextGatewayID   uint64
	permissions     map[string]chan permission.ResolveParams
	activeRuns      map[string]context.CancelFunc
	provider        provider.Provider
	tools           agenttools.ToolRunner
	workerPool      *worker.Pool
	mcp             *agentmcp.Manager
	runTodos        map[string][]methods.TodoItemDTO
	runGoals        map[string]*runGoalState
}

func New(in io.Reader, out io.Writer, log io.Writer, version string) *Runtime {
	rt := &Runtime{
		in:             in,
		out:            out,
		log:            log,
		version:        version,
		nextSeq:        map[string]uint64{},
		agentSeq:       map[string]map[string]uint64{},
		gatewayPending: map[jsonrpc.ID]chan jsonrpc.Response{},
		permissions:    map[string]chan permission.ResolveParams{},
		activeRuns:     map[string]context.CancelFunc{},
		mcp:            agentmcp.NewManager(version, log),
		runTodos:       map[string][]methods.TodoItemDTO{},
		runGoals:       map[string]*runGoalState{},
		provider:       provider.NewFromEnv(log),
		tools:          agenttools.ToolRunner{},
	}
	rt.tools.MemoryExecutor = rt.executeMemoryTool
	rt.tools.TodoExecutor = rt.todoExecutor
	rt.tools.GoalExecutor = rt.goalExecutor
	rt.tools.ContextExecutor = rt.executeContextTool
	rt.tools.SkillExecutor = rt.executeSkillRun
	rt.tools.WorkerDelegate = rt.executeWorkerDelegate
	rt.tools.WorkerList = rt.executeWorkerList
	rt.tools.WorkerCancel = rt.executeWorkerCancel
	rt.tools.WorkerPoolStatus = rt.executeWorkerPoolStatus
	rt.tools.WorkerSend = rt.executeWorkerSend
	rt.tools.WorkerReceive = rt.executeWorkerReceive
	rt.tools.MCPExecutor = rt.executeMCPTool
	workerPool, err := worker.NewPool(worker.Config{Size: workerPoolSizeFromEnv()}, func(workerID worker.WorkerID) (worker.Executor, error) {
		return newLazyProcessExecutor(rt, workerID), nil
	})
	if err != nil {
		panic(fmt.Errorf("create WorkerPool: %w", err))
	}
	rt.workerPool = workerPool
	return rt
}

func durationFromEnvMillis(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

func (r *Runtime) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(r.in)
	scanner.Buffer(make([]byte, 64*1024), maxJSONRPCLineBytes)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := r.handleLine(ctx, line); err != nil {
			fmt.Fprintf(r.log, "handle json-rpc line: %v\n", err)
		}
	}
	return scanner.Err()
}

func (r *Runtime) handleLine(ctx context.Context, line []byte) error {
	var probe struct {
		JSONRPC string     `json:"jsonrpc"`
		ID      jsonrpc.ID `json:"id"`
		Method  string     `json:"method"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return r.writeResponse(jsonrpc.NewError("rpc-parse-error", -32700, "parse error"))
	}
	if probe.Method == "" && probe.ID != "" {
		return r.handleGatewayResponse(line)
	}

	var req jsonrpc.Request
	_ = json.Unmarshal(line, &req)
	if req.JSONRPC != jsonrpc.Version || req.Method == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32600, "invalid request"))
	}

	switch req.Method {
	case methods.CoreInitialize:
		return r.handleInitialize(req)
	case methods.CorePing:
		return r.handlePing(req)
	case methods.CoreShutdown:
		_ = r.Close(context.Background())
		resp, err := jsonrpc.NewResult(req.ID, map[string]bool{"accepted": true})
		if err != nil {
			return err
		}
		return r.writeResponse(resp)
	case methods.RunExecute:
		return r.handleRunExecute(ctx, req)
	case methods.MCPDiscover:
		return r.handleMCPDiscover(ctx, req)
	case methods.AgentSkills:
		return r.handleAgentSkills(req)
	case methods.AgentSkillLoad:
		return r.handleAgentSkillLoad(req)
	case methods.AgentSkillCreate:
		return r.handleAgentSkillCreate(req)
	case methods.AgentSkillUpdate:
		return r.handleAgentSkillUpdate(req)
	case methods.AgentSkillDelete:
		return r.handleAgentSkillDelete(req)
	case methods.RunCancel:
		return r.handleRunCancel(req)
	case methods.WorkerList:
		return r.handleWorkerList(req)
	case methods.WorkerAssignmentCancel:
		return r.handleWorkerAssignmentCancel(req)
	case methods.WorkerPoolStatus:
		return r.handleWorkerPoolStatus(req)
	case methods.PermissionResolve:
		return r.handlePermissionResolve(req)
	default:
		return r.writeResponse(jsonrpc.NewError(req.ID, -32601, "method not found"))
	}
}

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
				r.emitCancelled(params)
				return
			}
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message": "permission request was not resolved",
				"status":  "failed",
			})
			return
		}
		if decision.Decision != permission.DecisionApprove {
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
	if invocation, ok := r.tools.Parse(params.Input.Text, params.RunID); ok {
		result, output, ok := r.executeTool(ctx, params, invocation)
		if !ok {
			if ctx.Err() != nil {
				r.emitCancelled(params)
				return
			}
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message":      result.Error,
				"tool_call_id": result.ToolCallID,
				"tool_name":    result.Name,
				"status":       string(result.Status),
			})
			_ = r.emitEvent(ctx, params, events.EventFinish, nil, map[string]any{
				"status": string(result.Status),
			})
			return
		}
		providerInput = fmt.Sprintf("User request:\n%s\n\nTool %s result:\n%s", params.Input.Text, result.Name, output)
		toolHistory = append(toolHistory, provider.ToolExchange{Call: invocation.Call, Result: result})
	}

	// Bound Goals use the controller loop; goal.create may bind mid-run.
	seg := r.runWithGoalLoop(ctx, params, providerInput, toolHistory, messageID, streamID, &streamSeq)
	goalStreamNeedsFinal := r.deferGoalStreamFinal(params.RunID)
	if goalStreamNeedsFinal && ctx.Err() == nil {
		_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
			StreamID: streamID,
			Kind:     events.StreamMessage,
			Seq:      streamSeq,
			Final:    true,
		}, map[string]any{
			"message_id":    messageID,
			"delta":         "",
			"provider_name": r.provider.Name(),
		})
		streamSeq++
	}
	// 仅在根运行结束时清理快照，不能在分段中途清理。
	r.clearRunSnapshots(params.RunID)

	status := finishStatusFromLoopEnd(seg.Reason)
	if status != "completed" {
		if status == "cancelled" {
			r.emitCancelled(params)
		} else {
			_ = r.emitEvent(ctx, params, events.EventFinish, nil, map[string]any{
				"status":          status,
				"loop_end_reason": string(seg.Reason),
			})
		}
		return
	}

	if ctx.Err() != nil {
		r.emitCancelled(params)
		return
	}
	if ctx.Err() != nil {
		r.emitCancelled(params)
		return
	}
	finishPayload := map[string]any{
		"status":          "completed",
		"provider_name":   r.provider.Name(),
		"loop_end_reason": string(seg.Reason),
		"tool_turns":      seg.ToolTurns,
	}
	if seg.Reason == loopEndMaxTurns {
		finishPayload["max_turns_reached"] = true
	}
	_ = r.emitEvent(ctx, params, events.EventFinish, nil, finishPayload)
}

func (r *Runtime) emitCancelled(params methods.ReplyParams) {
	_ = r.emitEvent(context.Background(), params, events.EventFinish, nil, map[string]any{
		"status": "cancelled",
	})
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

func (r *Runtime) registerRun(runID string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.activeRuns[runID]; exists {
		return false
	}
	r.activeRuns[runID] = cancel
	return true
}

func (r *Runtime) unregisterRun(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.activeRuns, runID)
	delete(r.nextSeq, runID)
	delete(r.agentSeq, runID)
	if r.mcp != nil {
		r.mcp.ClearBindings(runID)
	}
}

func (r *Runtime) cancelRun(runID string) bool {
	return r.cancelRunCount(runID, "run cancelled") > 0
}

func (r *Runtime) cancelRunCount(runID string, reason string) int {
	r.mu.Lock()
	cancel := r.activeRuns[runID]
	r.mu.Unlock()

	cancelledAssignments := 0
	if r.workerPool != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "run cancelled"
		}
		cancelledAssignments = r.workerPool.CancelRun(context.Background(), runID, reason)
	}
	if cancel != nil {
		cancel()
		if cancelledAssignments == 0 {
			cancelledAssignments = 1
		}
	}
	return cancelledAssignments
}

// Close stops all active runs and releases Runtime-owned execution resources.
func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(r.activeRuns))
	for _, cancel := range r.activeRuns {
		cancels = append(cancels, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if r.mcp != nil {
		r.mcp.CloseAll()
	}
	if r.workerPool != nil {
		return r.workerPool.Close(ctx)
	}
	return nil
}

func (r *Runtime) emitEvent(ctx context.Context, params methods.ReplyParams, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	return r.emitAgentEvent(ctx, params, events.AgentRef{
		AgentID: "root",
		Role:    events.AgentRoleRoot,
		Path:    []string{"root"},
		Name:    "root",
	}, typ, stream, payload)
}

// emitAgentEvent emits a v0.2 Worker-scoped event using EnvelopeV2.
// The agent parameter is kept for minimal internal compatibility (Role/Name/Path) but hierarchy is ignored.
func (r *Runtime) emitAgentEvent(ctx context.Context, params methods.ReplyParams, agent events.AgentRef, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.eventMu.Lock()
	defer r.eventMu.Unlock()

	rootSeq := r.nextRootSeq(params.RunID)

	// v0.2+: ALWAYS emit EnvelopeV2 via RunEvent. No legacy root/subagent hierarchy.
	assignment := assignmentFromContext(ctx)
	assignmentID := string(assignment.AssignmentID)
	workerID := string(assignment.WorkerID)
	profileKey := agent.Name
	if params.Options.WorkerContext != nil {
		if assignmentID == "" {
			assignmentID = params.Options.WorkerContext.AssignmentID
		}
		if workerID == "" {
			workerID = params.Options.WorkerContext.WorkerID
		}
	}
	if workerID == "" {
		workerID = "worker-unassigned"
	}
	workerSeq := r.nextAgentSeq(params.RunID, workerID)
	body := cloneEventPayload(payload)
	if _, exists := body["visibility"]; !exists {
		body["visibility"] = "conversation"
	}
	env := events.EnvelopeV2{
		ProtocolVersion: events.ProtocolVersionV2,
		EventID:         fmt.Sprintf("evt_%s_%d", params.RunID, rootSeq),
		RunID:           params.RunID,
		SessionID:       params.Session.ID,
		AssignmentID:    assignmentID,
		Worker:          events.EventWorkerRef{ID: workerID, ProfileKey: profileKey},
		RunSeq:          rootSeq,
		WorkerSeq:       workerSeq,
		Stream:          stream,
		Type:            typ,
		Payload:         body,
		CreatedAt:       time.Now().UTC(),
	}
	note, err := jsonrpc.NewNotification(methods.RunEvent, env)
	if err != nil {
		return err
	}
	return r.writeNotification(note)
}

func cloneEventPayload(payload map[string]any) map[string]any {
	next := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		next[key] = value
	}
	return next
}

func (r *Runtime) nextRootSeq(runID string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq[runID]++
	return r.nextSeq[runID]
}

func (r *Runtime) nextAgentSeq(runID string, agentID string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentSeq[runID] == nil {
		r.agentSeq[runID] = map[string]uint64{}
	}
	r.agentSeq[runID][agentID]++
	return r.agentSeq[runID][agentID]
}

func (r *Runtime) writeResponse(resp jsonrpc.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = fmt.Fprintln(r.out, string(raw))
	return err
}

func (r *Runtime) writeNotification(note jsonrpc.Notification) error {
	raw, err := json.Marshal(note)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err = fmt.Fprintln(r.out, string(raw))
	return err
}
