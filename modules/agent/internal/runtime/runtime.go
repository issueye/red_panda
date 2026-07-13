package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

const (
	defaultProviderToolTurns = 12
	maxProviderToolTurnsCap  = 48
	maxJSONRPCLineBytes      = 4 * 1024 * 1024
)

var plannerStartDelay = durationFromEnvMillis("RED_PANDA_PLANNER_START_DELAY_MS", 10*time.Millisecond)
var plannerDraftDelay = durationFromEnvMillis("RED_PANDA_PLANNER_DRAFT_DELAY_MS", 120*time.Millisecond)

// effectiveProviderToolTurns returns the provider↔tool loop budget for one reply.
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

	mu                 sync.Mutex
	eventMu            sync.Mutex
	initialized        bool
	nextSeq            map[string]uint64
	agentSeq           map[string]map[string]uint64
	gatewayPending     map[jsonrpc.ID]chan jsonrpc.Response
	nextGatewayID      uint64
	permissions        map[string]chan permission.ResolveParams
	activeRuns         map[string]context.CancelFunc
	subagents          map[string]*runtimeSubAgent
	provider           Provider
	tools              ToolRunner
	processPool        *subAgentProcessPool
	mcpProcesses       map[*mcpProcess]struct{}
	runTodos           map[string][]methods.TodoItemDTO
	runGoals           map[string]*runGoalState
	runWorkerCount     map[string]int
	newProcessSubAgent func(context.Context, methods.ReplyParams, string) (processSubAgent, error)
}

type runtimeSubAgent struct {
	record methods.SubAgentRecord
	cancel context.CancelFunc
}

type processSubAgent interface {
	Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error
	Cancel(ctx context.Context, runID string, reason string) error
	Close(ctx context.Context) error
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
		subagents:      map[string]*runtimeSubAgent{},
		mcpProcesses:   map[*mcpProcess]struct{}{},
		runTodos:       map[string][]methods.TodoItemDTO{},
		runGoals:       map[string]*runGoalState{},
		runWorkerCount: map[string]int{},
		provider:       newProviderFromEnv(log),
		tools:          ToolRunner{},
	}
	rt.tools.MemoryExecutor = rt.executeMemoryTool
	rt.tools.TodoExecutor = rt.todoExecutor
	rt.tools.GoalExecutor = rt.goalExecutor
	rt.tools.SkillExecutor = rt.executeSkillRun
	rt.tools.SubagentExecutor = rt.executeSubagentRun
	rt.tools.SubagentManager = rt
	rt.newProcessSubAgent = rt.createProcessSubAgent
	rt.processPool = newSubAgentProcessPool(subAgentPoolSizeFromEnv(), rt.newProcessSubAgent)
	return rt
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
		r.closeMCPProcesses()
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

	// Always reload skills from disk for this conversation so newly created
	// skills are immediately available without restarting sessions/runtime.
	params.Options.SkillsContext = buildSkillsContext(params.Session.WorkingDir)
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
	var toolHistory []ToolExchange
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
		toolHistory = append(toolHistory, ToolExchange{Call: invocation.Call, Result: result})
	}

	var subAgentDone <-chan struct{}
	if params.Options.SpawnSubAgents {
		subAgentDone = r.startPlannerSubAgent(ctx, params)
	} else {
		done := make(chan struct{})
		close(done)
		subAgentDone = done
	}

	// Multi-segment when a Goal is bound (or becomes bound mid-run via goal.write).
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
	// Snapshots are cleared only on root-run terminal — not mid-segment.
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

// loopEndReason is the structured exit cause of one provider↔tool segment.
// It must not be collapsed into stringly "completed" for multi-segment outer loops.
type loopEndReason string

const (
	loopEndNoTools   loopEndReason = "no_tools"
	loopEndMaxTurns  loopEndReason = "max_turns"
	loopEndCancelled loopEndReason = "cancelled"
	loopEndFailed    loopEndReason = "failed"
	loopEndBudget    loopEndReason = "budget_exhausted"
)

// providerSegmentResult is one runProviderLoopSegment outcome.
type providerSegmentResult struct {
	Reason    loopEndReason
	ToolTurns int
	// History is the accumulated tool exchanges for carry_summarized / next segment.
	History []ToolExchange
}

func finishStatusFromLoopEnd(reason loopEndReason) string {
	switch reason {
	case loopEndCancelled:
		return "cancelled"
	case loopEndFailed, loopEndBudget:
		return "failed"
	case loopEndNoTools, loopEndMaxTurns:
		// External finish status stays "completed" when the segment produced a
		// recoverable answer (including max-turns synthesis). Distinct reason is
		// preserved on the finish payload as loop_end_reason.
		return "completed"
	default:
		return "completed"
	}
}

func (r *Runtime) emitMemoryInjected(ctx context.Context, params methods.ReplyParams) {
	if params.Options.MemoryContext == nil || len(params.Options.MemoryContext.Items) == 0 {
		return
	}
	ids := make([]string, 0, len(params.Options.MemoryContext.Items))
	for _, item := range params.Options.MemoryContext.Items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	_ = r.emitEvent(ctx, params, events.EventMemoryInjected, nil, map[string]any{
		"memory_ids":    ids,
		"count":         len(params.Options.MemoryContext.Items),
		"context_chars": len(params.Options.MemoryContext.Context),
	})
}

func (r *Runtime) emitSkillsInjected(ctx context.Context, params methods.ReplyParams) {
	if params.Options.SkillsContext == nil {
		return
	}
	names := make([]string, 0, len(params.Options.SkillsContext.Items))
	for _, item := range params.Options.SkillsContext.Items {
		if item.Name != "" {
			names = append(names, item.Name)
		}
	}
	_ = r.emitEvent(ctx, params, events.EventSkillsInjected, nil, map[string]any{
		"skill_names":   names,
		"count":         len(names),
		"context_chars": len(params.Options.SkillsContext.Context),
	})
}

// runProviderLoop is a thin wrapper kept for call sites/tests that only need
// the legacy string status. Prefer runProviderLoopSegment for new code.
func (r *Runtime) runProviderLoop(ctx context.Context, params methods.ReplyParams, input string, history []ToolExchange, messageID string, streamID string, streamSeq *uint64) string {
	seg := r.runProviderLoopSegment(ctx, params, input, history, messageID, streamID, streamSeq)
	return finishStatusFromLoopEnd(seg.Reason)
}

// runProviderLoopSegment runs one provider↔tool budget segment.
// It does NOT clear run snapshots and does NOT emit EventFinish — the outer
// emitRun / Goal multi-segment controller owns terminal cleanup and finish.
func (r *Runtime) runProviderLoopSegment(ctx context.Context, params methods.ReplyParams, input string, history []ToolExchange, messageID string, streamID string, streamSeq *uint64) providerSegmentResult {
	maxTurns := effectiveProviderToolTurns(params.Options)
	var rounds [][]ToolExchange
	if len(history) > 0 {
		// Initial pre-loop tools (if any) are treated as one round.
		rounds = append(rounds, append([]ToolExchange(nil), history...))
	}
	turnsUsed := 0
	for turn := 0; turn < maxTurns; turn++ {
		// Mid-loop: refresh Todo/Goal context from run snapshots.
		if ctxTodos := r.todoContextForRun(params.RunID); ctxTodos != nil {
			params.Options.TodoContext = ctxTodos
		}
		if ctxGoal := r.goalContextForRun(params.RunID); ctxGoal != nil {
			params.Options.GoalContext = ctxGoal
		}
		var requestedCalls []tools.Call
		emittedText := false
		flatHistory := flattenToolRounds(rounds)
		releaseProvider, permitErr := r.acquireProviderPermit(ctx, params)
		if permitErr != nil {
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message": permitErr.Error(), "status": "failed",
			})
			return providerSegmentResult{Reason: loopEndFailed, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
		err := r.provider.Complete(ctx, ProviderRequest{
			RunID:       params.RunID,
			Session:     params.Session,
			Input:       methods.ReplyInput{Text: input},
			Options:     params.Options,
			Tools:       availableToolsForOptions(r.tools.AvailableTools(), params.Options),
			ToolHistory: flatHistory,
			ToolRounds:  rounds,
		}, func(chunk ProviderChunk) error {
			return r.consumeProviderChunk(ctx, params, chunk, messageID, streamID, streamSeq, &requestedCalls, &emittedText)
		})
		releaseProvider()
		turnsUsed++
		if err != nil {
			if ctx.Err() != nil {
				return providerSegmentResult{Reason: loopEndCancelled, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
			}
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message":       err.Error(),
				"status":        "failed",
				"provider_name": r.provider.Name(),
			})
			return providerSegmentResult{Reason: loopEndFailed, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
		if ctx.Err() != nil {
			return providerSegmentResult{Reason: loopEndCancelled, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
		if len(requestedCalls) == 0 {
			if !emittedText && len(flatHistory) > 0 {
				// Retry once without tools so the model must produce a final answer.
				if r.retryFinalAnswer(ctx, params, input, rounds, messageID, streamID, streamSeq) {
					return providerSegmentResult{Reason: loopEndNoTools, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
				}
				fallback := recoveryAnswerForRun(params, flatHistory)
				_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
					StreamID: streamID,
					Kind:     events.StreamMessage,
					Seq:      *streamSeq,
					Final:    !r.deferGoalStreamFinal(params.RunID),
				}, map[string]any{
					"message_id":    messageID,
					"delta":         fallback,
					"provider_name": r.provider.Name(),
					"recovered":     true,
				})
				(*streamSeq)++
			}
			return providerSegmentResult{Reason: loopEndNoTools, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
		// Preserve call order in history; run multiple subagent.run workers concurrently.
		exchanges, cancelled := r.executeToolBatch(ctx, params, requestedCalls)
		if len(exchanges) > 0 {
			rounds = append(rounds, exchanges)
		}
		if cancelled {
			return providerSegmentResult{Reason: loopEndCancelled, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
	}
	// Budget exhausted after tools — still try a final text-only synthesis.
	// Reason stays max_turns so Goal outer loops can open another segment.
	if len(rounds) > 0 {
		if r.retryFinalAnswer(ctx, params, input, rounds, messageID, streamID, streamSeq) {
			return providerSegmentResult{Reason: loopEndMaxTurns, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
		}
		fallback := recoveryAnswerForRun(params, flattenToolRounds(rounds))
		_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
			StreamID: streamID,
			Kind:     events.StreamMessage,
			Seq:      *streamSeq,
			Final:    !r.deferGoalStreamFinal(params.RunID),
		}, map[string]any{
			"message_id":    messageID,
			"delta":         fallback,
			"provider_name": r.provider.Name(),
			"recovered":     true,
			"max_turns":     maxTurns,
		})
		(*streamSeq)++
		return providerSegmentResult{Reason: loopEndMaxTurns, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
	}
	_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
		"message":   fmt.Sprintf("provider exceeded tool turn limit (%d)", maxTurns),
		"status":    "failed",
		"max_turns": maxTurns,
	})
	return providerSegmentResult{Reason: loopEndFailed, ToolTurns: turnsUsed, History: flattenToolRounds(rounds)}
}

// carrySummarizedHistory compresses tool exchanges for the next Goal segment.
// K most recent exchanges keep truncated outputs (rule-only, no LLM).
func carrySummarizedHistory(history []ToolExchange, k int, maxRunes int) []ToolExchange {
	if k <= 0 || len(history) == 0 {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 500
	}
	start := 0
	if len(history) > k {
		start = len(history) - k
	}
	out := make([]ToolExchange, 0, len(history)-start)
	for _, ex := range history[start:] {
		cp := ex
		if len(cp.Result.Output) > maxRunes {
			// Output is string; truncate by runes for CJK-safe budgets.
			runes := []rune(cp.Result.Output)
			if len(runes) > maxRunes {
				cp.Result.Output = string(runes[:maxRunes]) + "…"
			}
		}
		out = append(out, cp)
	}
	return out
}

func (r *Runtime) consumeProviderChunk(
	ctx context.Context,
	params methods.ReplyParams,
	chunk ProviderChunk,
	messageID string,
	streamID string,
	streamSeq *uint64,
	requestedCalls *[]tools.Call,
	emittedText *bool,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(chunk.ToolCalls) > 0 {
		*requestedCalls = append(*requestedCalls, chunk.ToolCalls...)
		return nil
	}
	if strings.TrimSpace(chunk.Delta) == "" && !chunk.Final {
		return nil
	}
	if strings.TrimSpace(chunk.Delta) != "" {
		*emittedText = true
	}
	if strings.TrimSpace(chunk.Delta) == "" && chunk.Final {
		return nil
	}
	err := r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamMessage,
		Seq:      *streamSeq,
		Final:    chunk.Final && !r.deferGoalStreamFinal(params.RunID),
	}, map[string]any{
		"message_id":    messageID,
		"delta":         chunk.Delta,
		"provider_name": r.provider.Name(),
	})
	(*streamSeq)++
	return err
}

// retryFinalAnswer asks the model once more without tools to produce a user-facing answer.
func (r *Runtime) retryFinalAnswer(
	ctx context.Context,
	params methods.ReplyParams,
	input string,
	rounds [][]ToolExchange,
	messageID string,
	streamID string,
	streamSeq *uint64,
) bool {
	if ctx.Err() != nil || len(rounds) == 0 {
		return false
	}
	recoveryPrompt := input + "\n\n[System] All tools for this turn have finished. " +
		"Write a complete, helpful final answer for the user based on the tool results above. " +
		"Do not call tools. Respond in the user's language. " +
		"If some tools failed, still summarize what succeeded and what is known."
	var answer strings.Builder
	returnedToolCalls := false
	releaseProvider, permitErr := r.acquireProviderPermit(ctx, params)
	if permitErr != nil {
		return false
	}
	err := r.provider.Complete(ctx, ProviderRequest{
		RunID:   params.RunID,
		Session: params.Session,
		Input:   methods.ReplyInput{Text: recoveryPrompt},
		Options: params.Options,
		// Force a text answer — no more tool calls.
		Tools:       nil,
		ToolHistory: flattenToolRounds(rounds),
		ToolRounds:  rounds,
	}, func(chunk ProviderChunk) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(chunk.ToolCalls) > 0 {
			returnedToolCalls = true
		}
		answer.WriteString(chunk.Delta)
		return nil
	})
	releaseProvider()
	text := strings.TrimSpace(answer.String())
	if err != nil || returnedToolCalls || !isUsableFinalText(text) {
		return false
	}
	err = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamMessage,
		Seq:      *streamSeq,
		Final:    !r.deferGoalStreamFinal(params.RunID),
	}, map[string]any{
		"message_id":    messageID,
		"delta":         text,
		"provider_name": r.provider.Name(),
		"recovered":     true,
	})
	(*streamSeq)++
	return err == nil
}

func flattenToolRounds(rounds [][]ToolExchange) []ToolExchange {
	if len(rounds) == 0 {
		return nil
	}
	total := 0
	for _, round := range rounds {
		total += len(round)
	}
	out := make([]ToolExchange, 0, total)
	for _, round := range rounds {
		out = append(out, round...)
	}
	return out
}

// synthesizeToolAnswer builds a visible user-facing summary when the provider
// ends a tool loop without emitting any natural-language reply.
// Prefer standardized tool envelopes (text/data) — never silently drop results.
func synthesizeToolAnswer(history []ToolExchange) string {
	if len(history) == 0 {
		return "工具已执行，但模型未生成最终回复。请重试一次。"
	}

	for _, exchange := range history {
		if exchange.Call.Name != "web.search" && exchange.Result.Name != "web.search" {
			continue
		}
		if text := extractSearchAnswer(exchange.Result.Output); text != "" {
			return text
		}
	}

	var b strings.Builder
	b.WriteString("根据工具执行结果整理如下：\n\n")
	for _, exchange := range history {
		name := strings.TrimSpace(exchange.Call.DisplayName)
		if name == "" {
			name = strings.TrimSpace(exchange.Call.Name)
		}
		if name == "" {
			name = "tool"
		}
		content := readableToolResultText(exchange.Result)
		if content == "" {
			content = "（无输出）"
		}
		if len(content) > 1200 {
			content = content[:1200] + "…\n[完整内容见工具卡片，未删除]"
		}
		b.WriteString("### ")
		b.WriteString(name)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func recoveryAnswerForRun(params methods.ReplyParams, history []ToolExchange) string {
	if strings.Contains(params.RunID, ":subagent:") {
		return "子代理已完成工具调用，但未生成可用的最终报告。工具结果已保留在工具卡片中。"
	}
	return synthesizeToolAnswer(history)
}

func readableToolResultText(result tools.Result) string {
	if env, ok := parseStandardToolResult(result.Output); ok {
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
		if strings.TrimSpace(env.Error) != "" {
			return strings.TrimSpace(env.Error)
		}
		if env.Data != nil {
			return preferReadableText(env.Data, "")
		}
	}
	return strings.TrimSpace(result.Output)
}

func extractSearchAnswer(raw string) string {
	if env, ok := parseStandardToolResult(raw); ok {
		if m, ok := env.Data.(map[string]any); ok {
			if built := formatSearchData(m); built != "" {
				return built
			}
		}
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return formatSearchData(parsed)
}

func formatSearchData(v map[string]any) string {
	answer, _ := v["answer"].(string)
	items, _ := v["items"].([]any)
	if strings.TrimSpace(answer) == "" && len(items) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(answer) != "" {
		b.WriteString(strings.TrimSpace(answer))
		b.WriteString("\n\n")
	}
	if len(items) > 0 {
		b.WriteString("参考来源：\n")
		limit := len(items)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			item, ok := items[i].(map[string]any)
			if !ok {
				continue
			}
			title, _ := item["title"].(string)
			url, _ := item["url"].(string)
			snippet, _ := item["snippet"].(string)
			b.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, strings.TrimSpace(title), strings.TrimSpace(url)))
			if snip := strings.TrimSpace(snippet); snip != "" {
				b.WriteString("   ")
				b.WriteString(compactOneLine(snip, 160))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func (r *Runtime) emitCancelled(params methods.ReplyParams) {
	_ = r.emitEvent(context.Background(), params, events.EventFinish, nil, map[string]any{
		"status": "cancelled",
	})
}

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
	initialStatus := "starting"
	initialSummary := backend + " planner subagent starting"
	if backend == "process_pool" {
		initialStatus = "queued"
		initialSummary = backend + " planner subagent queued"
	}
	_ = r.emitAgentEvent(ctx, params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      initialStatus,
		"summary":     initialSummary,
		"backend":     backend,
	})
	releasePermit, err := r.acquireWorkerPermit(ctx, params, subAgentID)
	if err != nil {
		r.finishSubAgent(params.RunID, subAgentID, "failed", backend+" planner worker permit failed", err.Error())
		_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
			"subagent_id": subAgentID,
			"name":        "planner",
			"status":      "failed",
			"summary":     backend + " planner worker permit failed",
			"backend":     backend,
			"error":       err.Error(),
		})
		return
	}
	defer releasePermit()

	child, release, err := r.acquireProcessSubAgent(ctx, params, subAgentID, backend, func() {
		r.setSubAgentStatus(params.RunID, subAgentID, "starting", backend+" planner subagent starting")
		_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
			"subagent_id": subAgentID,
			"name":        "planner",
			"status":      "starting",
			"summary":     backend + " planner subagent starting",
			"backend":     backend,
		})
	})
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
	r.setSubAgentStatus(params.RunID, subAgentID, "running", backend+" planner subagent running")
	_ = r.emitAgentEvent(context.Background(), params, agent, events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        "planner",
		"status":      "running",
		"summary":     backend + " planner subagent running",
		"backend":     backend,
	})

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

func (r *Runtime) acquireWorkerPermit(ctx context.Context, params methods.ReplyParams, subAgentID string) (func(), error) {
	if !params.Options.WorkerPermitRequired {
		return func() {}, nil
	}
	permit := methods.WorkerPermitParams{
		PermitID:  params.RunID + ":" + subAgentID,
		RootRunID: params.RunID,
		WorkerID:  subAgentID,
	}
	raw, err := r.callGateway(ctx, methods.WorkerPermitAcquire, permit)
	if err != nil {
		return nil, err
	}
	var result methods.WorkerPermitResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode worker permit: %w", err)
	}
	if !result.Granted {
		return nil, fmt.Errorf("worker permit was not granted")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = r.callGateway(releaseCtx, methods.WorkerPermitRelease, permit)
		})
	}, nil
}

func (r *Runtime) acquireProviderPermit(ctx context.Context, params methods.ReplyParams) (func(), error) {
	if !params.Options.ProviderPermitRequired {
		return func() {}, nil
	}
	rootRunID := params.RunID
	if index := strings.Index(rootRunID, ":subagent:"); index > 0 {
		rootRunID = rootRunID[:index]
	}
	permit := methods.WorkerPermitParams{
		PermitID:  fmt.Sprintf("%s:provider:%d", params.RunID, time.Now().UnixNano()),
		RootRunID: rootRunID,
		WorkerID:  params.RunID,
	}
	raw, err := r.callGateway(ctx, methods.ProviderPermitAcquire, permit)
	if err != nil {
		return nil, err
	}
	var result methods.WorkerPermitResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode provider permit: %w", err)
	}
	if !result.Granted {
		return nil, fmt.Errorf("provider permit was not granted")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = r.callGateway(releaseCtx, methods.ProviderPermitRelease, permit)
		})
	}, nil
}

func (r *Runtime) acquireProcessSubAgent(
	ctx context.Context,
	params methods.ReplyParams,
	subAgentID string,
	backend string,
	onStarting func(),
) (processSubAgent, func(bool), error) {
	if backend == "process_pool" {
		return r.processPool.AcquireWithStart(ctx, params, subAgentID, onStarting)
	}
	if onStarting != nil {
		onStarting()
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
	status := "running"
	summary := name + " subagent started"
	if backend == "process_pool" {
		status = "queued"
		summary = name + " subagent queued"
	} else if backend == "runtime_process" {
		status = "starting"
		summary = name + " subagent starting"
	}
	record := methods.SubAgentRecord{
		SubAgentID:      subAgentID,
		Name:            name,
		Backend:         backend,
		Status:          status,
		RootRunID:       params.RunID,
		ParentRunID:     params.RunID,
		ParentSessionID: params.Session.ID,
		ChildRunID:      params.RunID + ":subagent:" + subAgentID,
		Summary:         summary,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.mu.Lock()
	r.subagents[subAgentID] = &runtimeSubAgent{record: record, cancel: cancel}
	r.mu.Unlock()
}

func (r *Runtime) setSubAgentStatus(rootRunID string, subAgentID string, status string, summary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.subagents[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return
	}
	switch state.record.Status {
	case "cancelled", "failed", "completed":
		return
	}
	state.record.Status = status
	state.record.Summary = summary
	state.record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
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

type toolBatchItem struct {
	index      int
	call       tools.Call
	invocation ToolInvocation
	result     tools.Result
	ok         bool
}

// executeToolBatch preserves provider call order. Only adjacent subagent.run
// calls form a parallel group; ordinary tools are ordering barriers.
func (r *Runtime) executeToolBatch(ctx context.Context, params methods.ReplyParams, calls []tools.Call) ([]ToolExchange, bool) {
	items := make([]toolBatchItem, len(calls))

	for index, call := range calls {
		items[index] = toolBatchItem{index: index, call: call}
	}

	runOne := func(index int) {
		call := items[index].call
		invocation, err := r.tools.InvocationFromCall(params.RunID, index, call)
		if err != nil {
			failed := tools.Result{
				ToolCallID: call.ID,
				Name:       call.Name,
				Status:     tools.CallStatusFailed,
				Error:      err.Error(),
			}
			_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(failed))
			items[index].result = failed
			items[index].ok = false
			return
		}
		items[index].invocation = invocation
		result, _, ok := r.executeTool(ctx, params, invocation)
		items[index].result = result
		items[index].ok = ok
	}

	for index := 0; index < len(items); {
		if ctx.Err() != nil {
			return batchToHistory(items), true
		}
		if items[index].call.Name != "subagent.run" {
			runOne(index)
			index++
			continue
		}

		end := index + 1
		for end < len(items) && items[end].call.Name == "subagent.run" {
			end++
		}
		var wg sync.WaitGroup
		for workerIndex := index; workerIndex < end; workerIndex++ {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if ctx.Err() != nil {
					return
				}
				runOne(i)
			}(workerIndex)
		}
		wg.Wait()
		index = end
	}

	return batchToHistory(items), ctx.Err() != nil
}

func batchToHistory(items []toolBatchItem) []ToolExchange {
	history := make([]ToolExchange, 0, len(items))
	for _, item := range items {
		call := item.call
		if item.invocation.Call.ID != "" {
			call = item.invocation.Call
		}
		// Skip slots never executed (e.g. cancelled before parallel start).
		if item.result.ToolCallID == "" && item.result.Name == "" && item.result.Error == "" && item.result.Status == "" {
			if item.call.ID == "" && item.call.Name == "" {
				continue
			}
			// Still record cancelled/not-started calls as failed for model continuity.
			if item.result.Status == "" {
				item.result = tools.Result{
					ToolCallID: item.call.ID,
					Name:       item.call.Name,
					Status:     tools.CallStatusFailed,
					Error:      "tool was not executed",
				}
			}
		}
		history = append(history, ToolExchange{Call: call, Result: item.result})
	}
	return history
}

func (r *Runtime) executeTool(ctx context.Context, params methods.ReplyParams, invocation ToolInvocation) (tools.Result, string, bool) {
	call := invocation.Call
	decision := EvaluateToolPolicy(params.Options, call)
	_ = r.emitEvent(ctx, params, events.EventToolStarted, nil, map[string]any{
		"tool_call_id":  call.ID,
		"tool_name":     call.Name,
		"display_name":  call.DisplayName,
		"risk":          string(call.Risk),
		"arguments":     call.Arguments,
		"status":        string(tools.CallStatusRunning),
		"policy":        string(decision.Action),
		"policy_reason": decision.Reason,
	})

	if decision.Action == ToolDecisionDeny {
		result := tools.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Status:     tools.CallStatusDenied,
			Error:      decision.Reason,
		}
		_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, "", false
	}

	if decision.Action == ToolDecisionRequirePermission {
		decision, ok := r.requestPermission(ctx, params, permission.RequestPayload{
			PermissionID: "perm_" + call.ID,
			RunID:        params.RunID,
			ToolCallID:   call.ID,
			ToolName:     call.Name,
			Risk:         string(call.Risk),
			Summary:      fmt.Sprintf("Allow %s to run.", call.DisplayName),
			Detail:       "The Agent Runtime is waiting for permission before executing this tool.",
			Arguments:    call.Arguments,
		})
		if !ok || decision.Decision != permission.DecisionApprove {
			result := tools.Result{
				ToolCallID: call.ID,
				Name:       call.Name,
				Status:     tools.CallStatusDenied,
				Error:      "permission denied",
			}
			_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
			return result, "", false
		}
	}

	result, output := r.tools.RunWithContext(ctx, ToolRunContext{
		WorkingDir: params.Session.WorkingDir,
		RunID:      params.RunID,
		SessionID:  params.Session.ID,
		Reply:      &params,
	}, invocation)
	if output != "" {
		streamKind := events.StreamToolStdout
		if result.Status == tools.CallStatusFailed {
			streamKind = events.StreamToolStderr
		}
		_ = r.emitEvent(ctx, params, events.EventToolOutput, &events.StreamRef{
			StreamID: "stream_" + call.ID,
			Kind:     streamKind,
			Seq:      1,
			Final:    true,
		}, map[string]any{
			"tool_call_id": call.ID,
			"tool_name":    call.Name,
			"delta":        output,
		})
	}
	if result.Status == tools.CallStatusFailed {
		_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(result))
		return result, output, false
	}
	_ = r.emitEvent(ctx, params, events.EventToolFinished, nil, toolResultPayload(result))
	if call.Name == "todo.write" || call.Name == "todo_write" {
		items := r.getRunTodos(params.RunID)
		open, completed, cancelled := todoStatusCounts(items)
		_ = r.emitEvent(ctx, params, events.EventTodoUpdated, nil, map[string]any{
			"session_id":      params.Session.ID,
			"run_id":          params.RunID,
			"tool_call_id":    call.ID,
			"action":          "write",
			"items":           items,
			"open_count":      open,
			"completed_count": completed,
			"cancelled_count": cancelled,
		})
	}
	switch call.Name {
	case "goal.write", "goal.update", "goal.checkpoint", "goal.complete":
		if state := r.getRunGoal(params.RunID); state != nil && state.Goal.ID != "" {
			_ = r.emitEvent(ctx, params, events.EventGoalUpdated, nil, map[string]any{
				"session_id":     params.Session.ID,
				"run_id":         params.RunID,
				"tool_call_id":   call.ID,
				"action":         strings.TrimPrefix(call.Name, "goal."),
				"goal":           state.Goal,
				"pipeline_phase": state.Goal.PipelinePhase,
				"status":         state.Goal.Status,
			})
		}
	}
	return result, output, true
}

func todoStatusCounts(items []methods.TodoItemDTO) (open, completed, cancelled int) {
	for _, item := range items {
		switch item.Status {
		case "pending", "in_progress":
			open++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}
	return
}

func (r *Runtime) executeMemoryTool(ctx context.Context, req methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	raw, err := r.callGateway(ctx, methods.MemoryToolExecute, req)
	if err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	var result methods.MemoryToolExecuteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) todoExecutor(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	result, err := r.executeTodoTool(ctx, req)
	if err != nil {
		return result, err
	}
	name := strings.TrimSpace(req.ToolName)
	if name == "todo.write" || name == "todo_write" {
		r.setRunTodos(req.RunID, result.Items)
	}
	return result, nil
}

func (r *Runtime) executeTodoTool(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	raw, err := r.callGateway(ctx, methods.TodoToolExecute, req)
	if err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	var result methods.TodoToolExecuteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) goalExecutor(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	result, err := r.executeGoalTool(ctx, req)
	if err != nil {
		return result, err
	}
	name := strings.TrimSpace(req.ToolName)
	if name != "goal.list" && name != "segment_end" {
		r.applyGoalToolResult(req.RunID, name, result)
	} else if name == "segment_end" {
		r.applyGoalToolResult(req.RunID, name, result)
	}
	return result, nil
}

func (r *Runtime) executeGoalTool(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	raw, err := r.callGateway(ctx, methods.GoalToolExecute, req)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	var result methods.GoalToolExecuteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) setRunTodos(runID string, items []methods.TodoItemDTO) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runTodos == nil {
		r.runTodos = map[string][]methods.TodoItemDTO{}
	}
	copied := append([]methods.TodoItemDTO(nil), items...)
	r.runTodos[runID] = copied
}

func (r *Runtime) getRunTodos(runID string) []methods.TodoItemDTO {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.runTodos[runID]
	if len(items) == 0 {
		return nil
	}
	return append([]methods.TodoItemDTO(nil), items...)
}

func (r *Runtime) clearRunTodos(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runTodos, runID)
}

func (r *Runtime) todoContextForRun(runID string) *methods.TodoContext {
	items := r.getRunTodos(runID)
	if len(items) == 0 {
		return nil
	}
	return formatTodoContext(items)
}

const todoContextHeader = `Current session task list (update via todo.write when progress changes; keep one in_progress):
`

func formatTodoContext(items []methods.TodoItemDTO) *methods.TodoContext {
	const limit = 3000
	const maxLine = 200
	// open-first
	ordered := append([]methods.TodoItemDTO(nil), items...)
	sortTodoItemsOpenFirst(ordered)
	lines := make([]string, 0, len(ordered))
	kept := make([]methods.TodoItemDTO, 0, len(ordered))
	header := todoContextHeader
	total := len(header)
	for _, item := range ordered {
		key := item.ClientKey
		if key == "" {
			key = item.ID
		}
		line := fmt.Sprintf("- [%s] %s (%s)", item.Status, item.Content, key)
		runes := []rune(line)
		if len(runes) > maxLine {
			line = string(runes[:maxLine-1]) + "…"
		}
		if total+len(line)+1 > limit {
			break
		}
		lines = append(lines, line)
		kept = append(kept, item)
		total += len(line) + 1
	}
	if len(lines) == 0 {
		return nil
	}
	return &methods.TodoContext{
		Items:   kept,
		Context: header + strings.Join(lines, "\n"),
	}
}

func sortTodoItemsOpenFirst(items []methods.TodoItemDTO) {
	rank := func(status string) int {
		switch status {
		case "in_progress":
			return 0
		case "pending":
			return 1
		case "completed":
			return 2
		case "cancelled":
			return 3
		default:
			return 9
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if rank(items[j].Status) < rank(items[i].Status) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (r *Runtime) callGateway(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := r.nextGatewayRequestID()
	req, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan jsonrpc.Response, 1)
	r.mu.Lock()
	r.gatewayPending[id] = ch
	_, err = fmt.Fprintln(r.out, string(raw))
	r.mu.Unlock()
	if err != nil {
		r.removeGatewayPending(id)
		return nil, err
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.removeGatewayPending(id)
		return nil, ctx.Err()
	case <-timer.C:
		r.removeGatewayPending(id)
		return nil, fmt.Errorf("gateway request %s timed out", method)
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("gateway response channel closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("gateway error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (r *Runtime) nextGatewayRequestID() jsonrpc.ID {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGatewayID++
	return jsonrpc.ID(fmt.Sprintf("rt_%d", r.nextGatewayID))
}

func (r *Runtime) removeGatewayPending(id jsonrpc.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.gatewayPending, id)
}

func toolResultPayload(result tools.Result) map[string]any {
	return map[string]any{
		"tool_call_id": result.ToolCallID,
		"tool_name":    result.Name,
		"status":       string(result.Status),
		"exit_code":    result.ExitCode,
		"output":       result.Output,
		"error":        result.Error,
		"duration_ms":  result.DurationMS,
	}
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
	delete(r.runWorkerCount, runID)
}

func (r *Runtime) reserveWorkerFanOut(runID string, limit int) bool {
	if limit <= 0 {
		limit = 8
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runWorkerCount[runID] >= limit {
		return false
	}
	r.runWorkerCount[runID]++
	return true
}

func (r *Runtime) cancelRun(runID string) bool {
	r.mu.Lock()
	cancel := r.activeRuns[runID]
	// Pause every subagent bound to this root run before cancelling the parent
	// so process-pool workers stop promptly (not only via shared context).
	var subCancels []context.CancelFunc
	for _, state := range r.subagents {
		if state == nil || state.record.RootRunID != runID || state.cancel == nil {
			continue
		}
		subCancels = append(subCancels, state.cancel)
	}
	r.mu.Unlock()

	for _, subCancel := range subCancels {
		subCancel()
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
