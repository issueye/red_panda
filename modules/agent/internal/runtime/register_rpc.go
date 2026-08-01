package runtime

// register_rpc.go — JSON-RPC method registration for the Runtime Dispatcher.
// Replaces the handleLine switch in runtime.go (docs/53 §6.3).
//
// Protocol compatibility: method names and params are identical to v0.2.x.
//
// MethodHandler signature: (Response, error)
//   Response != nil  → "write this response"
//   error != nil     → "internal error (handler may have already written)"
//
// Two handler styles:
//   - Inline: returns (jsonrpc.NewResult/... , nil) directly
//   - Delegate: returns (nil, r.handleXXX(ctx, req)) — existing handler writes
//     its own response via r.writeResponse

import (
	"context"
	"encoding/json"

	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
)

func initRPCDispatcher(r *Runtime) *registry.Dispatcher {
	d := registry.NewDispatcher()

	// Core methods (inline — cleanest)
	d.Register(methods.CoreInitialize, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.InitializeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		if params.ProtocolVersion != events.ProtocolVersionV2 {
			return jsonrpc.NewError(req.ID, -32001, "incompatible protocol version"), nil
		}
		r.mu.Lock()
		r.initialized = true
		r.protocolVersion = events.ProtocolVersionV2
		r.mu.Unlock()
		result := methods.InitializeResult{
			ProtocolVersion: events.ProtocolVersionV2,
			Server:          methods.PeerInfo{Name: "red-panda-agent", Version: r.version},
			Capabilities:    r.initCapabilities(),
		}
		return jsonrpc.NewResult(req.ID, result)
	})

	d.Register(methods.CorePing, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.PingParams
		_ = json.Unmarshal(req.Params, &params)
		return jsonrpc.NewResult(req.ID, methods.PingResult{Nonce: params.Nonce, Status: "ok"})
	})

	d.Register(methods.CoreShutdown, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		_ = r.Close(context.Background())
		return jsonrpc.NewResult(req.ID, map[string]bool{"accepted": true})
	})

	// Methods that write their own response (delegate to existing handlers)
	d.Register(methods.RunExecute, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleRunExecute(ctx, req)
	})
	d.Register(methods.MCPDiscover, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleMCPDiscover(ctx, req)
	})
	d.Register(methods.MCPCall, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleMCPCall(ctx, req)
	})
	d.Register(methods.AgentSkills, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleAgentSkills(req)
	})
	d.Register(methods.AgentSkillLoad, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleAgentSkillLoad(req)
	})
	d.Register(methods.AgentSkillCreate, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleAgentSkillCreate(req)
	})
	d.Register(methods.AgentSkillUpdate, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleAgentSkillUpdate(req)
	})
	d.Register(methods.AgentSkillDelete, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.Response{}, r.handleAgentSkillDelete(req)
	})

	// Worker / Run methods (inline for clean Response handling)
	d.Register(methods.RunCancel, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.RunCancelParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		cancelled := r.cancelRunCount(params.RunID, params.Reason)
		return jsonrpc.NewResult(req.ID, methods.RunCancelResult{
			Accepted: true, RunID: params.RunID, Cancelled: cancelled,
		})
	})
	d.Register(methods.RunPause, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.RunPauseParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		paused := 0
		var err error
		if params.DelegatedOnly {
			paused, err = r.workerPool.PauseDelegatedRun(context.Background(), params.RunID, params.Reason)
		} else if r.runStates.Pause(params.RunID) {
			paused = 1
		}
		if err != nil {
			return jsonrpc.NewError(req.ID, -32020, err.Error()), nil
		}
		return jsonrpc.NewResult(req.ID, methods.RunPauseResult{Accepted: true, RunID: params.RunID, Paused: paused})
	})
	d.Register(methods.RunResume, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.RunResumeParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		resumed := 0
		var err error
		if params.DelegatedOnly {
			resumed, err = r.workerPool.ResumeDelegatedRun(context.Background(), params.RunID)
		} else if r.runStates.Resume(params.RunID) {
			resumed = 1
		}
		if err != nil {
			return jsonrpc.NewError(req.ID, -32021, err.Error()), nil
		}
		return jsonrpc.NewResult(req.ID, methods.RunResumeResult{Accepted: true, RunID: params.RunID, Resumed: resumed})
	})
	d.Register(methods.WorkerList, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.WorkerListParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
			}
		}
		snapshot := r.workerPool.Snapshot()
		result := methods.WorkerListResult{}
		for _, item := range snapshot.Workers {
			if params.WorkerID != "" && string(item.ID) != params.WorkerID {
				continue
			}
			result.Workers = append(result.Workers, methods.WorkerRef{
				ID: string(item.ID), State: methods.WorkerState(item.State),
				CurrentAssignmentID: string(item.CurrentAssignmentID),
				ProfileKey:          item.ProfileKey, MailboxDepth: item.MailboxDepth,
				MailboxCapacity: item.MailboxCapacity, Healthy: item.Healthy,
			})
		}
		for _, item := range snapshot.Assignments {
			if params.AssignmentID != "" && string(item.ID) != params.AssignmentID {
				continue
			}
			if params.WorkerID != "" && string(item.WorkerID) != params.WorkerID {
				continue
			}
			result.Assignments = append(result.Assignments, methods.AssignmentRecord{
				ID: string(item.ID), RunID: item.RunID, WorkerID: string(item.WorkerID),
				OriginWorkerID: string(item.OriginWorkerID), ProfileKey: item.ProfileKey,
				Task: item.Task, Status: methods.AssignmentStatus(item.Status),
				Result: item.Result, Error: item.Error,
				ExecutionStats: methodWorkerExecutionStats(item.Stats),
				CreatedAt:      item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
			})
		}
		if result.Workers == nil {
			result.Workers = []methods.WorkerRef{}
		}
		if result.Assignments == nil {
			result.Assignments = []methods.AssignmentRecord{}
		}
		return jsonrpc.NewResult(req.ID, result)
	})
	d.Register(methods.WorkerAssignmentCancel, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params methods.WorkerAssignmentCancelParams
		if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" || params.AssignmentID == "" {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		found := false
		for _, a := range r.workerPool.Snapshot().Assignments {
			if a.ID == worker.AssignmentID(params.AssignmentID) && a.RunID == params.RunID {
				found = true
				break
			}
		}
		cancelled := false
		if found {
			cancelled = r.workerPool.Cancel(context.Background(), worker.AssignmentID(params.AssignmentID), params.Reason)
		}
		return jsonrpc.NewResult(req.ID, methods.WorkerAssignmentCancelResult{
			Accepted: true, RunID: params.RunID, AssignmentID: params.AssignmentID, Cancelled: cancelled,
		})
	})
	d.Register(methods.WorkerPoolStatus, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		snapshot := r.workerPool.Snapshot()
		return jsonrpc.NewResult(req.ID, methods.WorkerPoolStatusResult{
			Pool: methods.PoolSnapshot{
				Configured: snapshot.Configured, Ready: snapshot.Ready, Busy: snapshot.Busy,
				Draining: snapshot.Draining, Unhealthy: snapshot.Unhealthy, Stopped: snapshot.Stopped,
				Queued: snapshot.Queued, Running: snapshot.Running,
				WaitingPermission: snapshot.WaitingPermission, Paused: snapshot.Paused,
			},
		})
	})

	// Permission resolve (inline)
	d.Register(methods.PermissionResolve, func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		_ = ctx
		var params permission.ResolveParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return jsonrpc.NewError(req.ID, -32602, "invalid params"), nil
		}
		if params.PermissionID == "" {
			return jsonrpc.NewError(req.ID, -32602, "missing permission_id"), nil
		}
		r.mu.Lock()
		ch := r.permissions[params.PermissionID]
		r.mu.Unlock()
		if ch == nil {
			return jsonrpc.NewError(req.ID, -32004, "permission request not found"), nil
		}
		ch <- params
		return jsonrpc.NewResult(req.ID, permission.ResolveResult{Accepted: true})
	})

	return d
}

// initCapabilities returns the v0.2 capability list for initialize response.
func (r *Runtime) initCapabilities() []methods.Capability {
	return []methods.Capability{
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
	}
}
