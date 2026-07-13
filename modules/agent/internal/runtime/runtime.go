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
	"sync"
	"time"

	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
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

	mu                  sync.Mutex
	eventMu             sync.Mutex
	initialized         bool
	nextSeq             map[string]uint64
	agentSeq            map[string]map[string]uint64
	gatewayPending      map[jsonrpc.ID]chan jsonrpc.Response
	nextGatewayID       uint64
	permissions         map[string]chan permission.ResolveParams
	activeRuns          map[string]context.CancelFunc
	subagents           *subagent.Registry
	provider            provider.Provider
	tools               agenttools.ToolRunner
	processPool         *subagent.ProcessPool
	subagentCoordinator *subagent.Coordinator
	mcp                 *agentmcp.Manager
	runTodos            map[string][]methods.TodoItemDTO
	runGoals            map[string]*runGoalState
	newProcessSubAgent  func(context.Context, methods.ReplyParams, string) (subagent.Process, error)
}

var _ agenttools.SubagentManager = (*Runtime)(nil)

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
		subagents:      subagent.NewRegistry(),
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
	rt.tools.SubagentExecutor = rt.executeSubagentRun
	rt.tools.SubagentManager = rt
	rt.tools.MCPExecutor = rt.executeMCPTool
	rt.newProcessSubAgent = rt.createProcessSubAgent
	rt.processPool = subagent.NewProcessPool(subagent.PoolSizeFromEnv(), rt.newProcessSubAgent)
	coordinator, err := subagent.NewCoordinator(runtimeProcessProvider{runtime: rt}, runtimeEventSink{runtime: rt}, rt.subagents)
	if err != nil {
		panic(err)
	}
	rt.subagentCoordinator = coordinator
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
		if r.mcp != nil {
			r.mcp.CloseAll()
		}
		if r.processPool != nil {
			r.processPool.Close(context.Background())
		}
		resp, err := jsonrpc.NewResult(req.ID, map[string]bool{"accepted": true})
		if err != nil {
			return err
		}
		return r.writeResponse(resp)
	case methods.AgentReply:
		return r.handleReply(ctx, req)
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
	case methods.AgentCancel:
		return r.handleCancel(req)
	case methods.AgentSubAgents:
		return r.handleSubAgents(req)
	case methods.AgentSubAgentCancel:
		return r.handleSubAgentCancel(req)
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
	r.mu.Lock()
	r.initialized = true
	r.mu.Unlock()

	result := methods.InitializeResult{
		ProtocolVersion: events.ProtocolVersion,
		Server:          methods.PeerInfo{Name: "red-panda-agent", Version: r.version},
		Capabilities: []methods.Capability{
			{Name: methods.CorePing, Version: 1},
			{Name: methods.AgentTools, Version: 1},
			{Name: methods.AgentReply, Version: 1},
			{Name: methods.AgentCancel, Version: 1},
			{Name: methods.AgentSubAgents, Version: 1},
			{Name: methods.AgentSubAgentCancel, Version: 1},
			{Name: methods.AgentSkills, Version: 1},
			{Name: methods.AgentSkillLoad, Version: 1},
			{Name: methods.AgentSkillCreate, Version: 1},
			{Name: methods.AgentSkillUpdate, Version: 1},
			{Name: methods.AgentSkillDelete, Version: 1},
			{Name: methods.AgentEvent, Version: 1},
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

func (r *Runtime) handlePing(req jsonrpc.Request) error {
	var params methods.PingParams
	_ = json.Unmarshal(req.Params, &params)
	resp, err := jsonrpc.NewResult(req.ID, methods.PingResult{Nonce: params.Nonce, Status: "ok"})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleReply(ctx context.Context, req jsonrpc.Request) error {
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

	accepted, err := jsonrpc.NewResult(req.ID, methods.ReplyAccepted{Accepted: true, RunID: params.RunID})
	if err != nil {
		r.unregisterRun(params.RunID)
		cancel()
		return err
	}
	if err := r.writeResponse(accepted); err != nil {
		r.unregisterRun(params.RunID)
		cancel()
		return err
	}

	go func() {
		defer r.unregisterRun(params.RunID)
		r.emitRun(runCtx, params)
	}()
	return nil
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

	var subAgentDone <-chan struct{}
	if params.Options.SpawnSubAgents {
		subAgentDone = r.startPlannerSubAgent(ctx, params)
	} else {
		done := make(chan struct{})
		close(done)
		subAgentDone = done
	}

	// 绑定 Goal 时执行多分段流程；也支持通过 goal.write 在运行中途绑定。
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
		<-subAgentDone
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
	<-subAgentDone
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
	r.mu.Lock()
	cancel := r.activeRuns[runID]
	r.mu.Unlock()

	// 取消父运行前暂停绑定到该根运行的全部子代理，
	// 使进程池工作进程能及时停止，而非只依赖共享上下文。
	for _, record := range r.subagents.List(methods.SubAgentsParams{RunID: runID}) {
		r.subagents.Cancel(runID, record.SubAgentID)
	}
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *Runtime) emitEvent(ctx context.Context, params methods.ReplyParams, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	return r.emitAgentEvent(ctx, params, events.AgentRef{
		AgentID: "root",
		Role:    events.AgentRoleRoot,
		Path:    []string{"root"},
		Name:    "root",
	}, typ, stream, payload)
}

func (r *Runtime) emitAgentEvent(ctx context.Context, params methods.ReplyParams, agent events.AgentRef, typ events.EventType, stream *events.StreamRef, payload map[string]any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	r.eventMu.Lock()
	defer r.eventMu.Unlock()

	rootSeq := r.nextRootSeq(params.RunID)
	agentSeq := r.nextAgentSeq(params.RunID, agent.AgentID)
	eventRunID := params.RunID
	parentRunID := ""
	if agent.Role == events.AgentRoleSubAgent && agent.SubAgentID != "" {
		eventRunID = params.RunID + ":subagent:" + agent.SubAgentID
		parentRunID = params.RunID
	}
	env := events.Envelope{
		ProtocolVersion: events.ProtocolVersion,
		EventID:         fmt.Sprintf("evt_%s_%d", params.RunID, rootSeq),
		RootRunID:       params.RunID,
		RunID:           eventRunID,
		ParentRunID:     parentRunID,
		SessionID:       params.Session.ID,
		RootSeq:         rootSeq,
		AgentSeq:        agentSeq,
		Agent:           agent,
		Stream:          stream,
		Type:            typ,
		Payload:         payload,
		CreatedAt:       time.Now().UTC(),
	}
	note, err := jsonrpc.NewNotification(methods.AgentEvent, env)
	if err != nil {
		return err
	}
	return r.writeNotification(note)
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
