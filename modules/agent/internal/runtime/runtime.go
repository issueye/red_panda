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
	"sync"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

const maxProviderToolTurns = 4

var plannerStartDelay = durationFromEnvMillis("RED_PANDA_PLANNER_START_DELAY_MS", 10*time.Millisecond)
var plannerDraftDelay = durationFromEnvMillis("RED_PANDA_PLANNER_DRAFT_DELAY_MS", 120*time.Millisecond)

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
		provider:       newProviderFromEnv(log),
		tools:          ToolRunner{},
	}
	rt.tools.MemoryExecutor = rt.executeMemoryTool
	rt.tools.SkillExecutor = rt.executeSkillRun
	rt.newProcessSubAgent = newSubAgentProcess
	rt.processPool = newSubAgentProcessPool(subAgentPoolSizeFromEnv(), rt.newProcessSubAgent)
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
			{Name: methods.AgentEvent, Version: 1},
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

	status := r.runProviderLoop(ctx, params, providerInput, toolHistory, messageID, streamID, &streamSeq)
	if status != "completed" {
		<-subAgentDone
		if status == "cancelled" {
			r.emitCancelled(params)
		} else {
			_ = r.emitEvent(ctx, params, events.EventFinish, nil, map[string]any{
				"status": status,
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
	_ = r.emitEvent(ctx, params, events.EventFinish, nil, map[string]any{
		"status":        "completed",
		"provider_name": r.provider.Name(),
	})
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

func (r *Runtime) runProviderLoop(ctx context.Context, params methods.ReplyParams, input string, history []ToolExchange, messageID string, streamID string, streamSeq *uint64) string {
	for turn := 0; turn < maxProviderToolTurns; turn++ {
		var requestedCalls []tools.Call
		providerParams := params
		providerParams.Input.Text = input
		err := r.provider.Complete(ctx, ProviderRequest{
			RunID:       params.RunID,
			Session:     params.Session,
			Input:       providerParams.Input,
			Options:     providerParams.Options,
			Tools:       availableToolsForOptions(r.tools.AvailableTools(), providerParams.Options),
			ToolHistory: history,
		}, func(chunk ProviderChunk) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if len(chunk.ToolCalls) > 0 {
				requestedCalls = append(requestedCalls, chunk.ToolCalls...)
				return nil
			}
			err := r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
				StreamID: streamID,
				Kind:     events.StreamMessage,
				Seq:      *streamSeq,
				Final:    chunk.Final,
			}, map[string]any{
				"message_id":    messageID,
				"delta":         chunk.Delta,
				"provider_name": r.provider.Name(),
			})
			(*streamSeq)++
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return "cancelled"
			}
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message":       err.Error(),
				"status":        "failed",
				"provider_name": r.provider.Name(),
			})
			return "failed"
		}
		if ctx.Err() != nil {
			return "cancelled"
		}
		if len(requestedCalls) == 0 {
			return "completed"
		}
		for index, call := range requestedCalls {
			invocation, err := r.tools.InvocationFromCall(params.RunID, index, call)
			if err != nil {
				_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
					"message":   err.Error(),
					"status":    "failed",
					"tool_name": call.Name,
				})
				return "failed"
			}
			result, _, ok := r.executeTool(ctx, params, invocation)
			history = append(history, ToolExchange{Call: invocation.Call, Result: result})
			if !ok {
				if ctx.Err() != nil {
					return "cancelled"
				}
				_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
					"message":      result.Error,
					"tool_call_id": result.ToolCallID,
					"tool_name":    result.Name,
					"status":       string(result.Status),
				})
				return string(result.Status)
			}
		}
	}
	_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
		"message": "provider exceeded tool turn limit",
		"status":  "failed",
	})
	return "failed"
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

func (r *Runtime) acquireProcessSubAgent(ctx context.Context, params methods.ReplyParams, subAgentID string, backend string) (processSubAgent, func(bool), error) {
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
	return result, output, true
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
}

func (r *Runtime) cancelRun(runID string) bool {
	r.mu.Lock()
	cancel := r.activeRuns[runID]
	r.mu.Unlock()
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
