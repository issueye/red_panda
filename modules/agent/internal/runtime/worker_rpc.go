package runtime

import (
	"context"
	"encoding/json"
	"strings"

	"redpanda/agent/internal/worker"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func (r *Runtime) handleRunCancel(req jsonrpc.Request) error {
	var params methods.RunCancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.RunID) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	cancelled := r.cancelRunCount(params.RunID, params.Reason)
	resp, err := jsonrpc.NewResult(req.ID, methods.RunCancelResult{
		Accepted:  true,
		RunID:     params.RunID,
		Cancelled: cancelled,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleRunPause(req jsonrpc.Request) error {
	var params methods.RunPauseParams
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.RunID) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	paused := 0
	var err error
	if params.DelegatedOnly {
		paused, err = r.workerPool.PauseDelegatedRun(context.Background(), params.RunID, params.Reason)
	} else if r.runStates.Pause(params.RunID) {
		paused = 1
	}
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32020, err.Error()))
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.RunPauseResult{Accepted: true, RunID: params.RunID, Paused: paused})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleRunResume(req jsonrpc.Request) error {
	var params methods.RunResumeParams
	if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.RunID) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	resumed := 0
	var err error
	if params.DelegatedOnly {
		resumed, err = r.workerPool.ResumeDelegatedRun(context.Background(), params.RunID)
	} else if r.runStates.Resume(params.RunID) {
		resumed = 1
	}
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32021, err.Error()))
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.RunResumeResult{Accepted: true, RunID: params.RunID, Resumed: resumed})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleWorkerList(req jsonrpc.Request) error {
	var params methods.WorkerListParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
		}
	}
	snapshot := workerSnapshotForRun(r.workerPool.Snapshot(), strings.TrimSpace(params.RunID))
	result := methods.WorkerListResult{}
	for _, item := range snapshot.Workers {
		if params.WorkerID != "" && string(item.ID) != params.WorkerID {
			continue
		}
		result.Workers = append(result.Workers, methodWorkerRef(item))
	}
	for _, item := range snapshot.Assignments {
		if params.AssignmentID != "" && string(item.ID) != params.AssignmentID {
			continue
		}
		if params.WorkerID != "" && string(item.WorkerID) != params.WorkerID {
			continue
		}
		result.Assignments = append(result.Assignments, methodAssignmentRecord(item))
	}
	if result.Workers == nil {
		result.Workers = []methods.WorkerRef{}
	}
	if result.Assignments == nil {
		result.Assignments = []methods.AssignmentRecord{}
	}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleWorkerAssignmentCancel(req jsonrpc.Request) error {
	var params methods.WorkerAssignmentCancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" || params.AssignmentID == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	id := worker.AssignmentID(params.AssignmentID)
	found := false
	for _, assignment := range r.workerPool.Snapshot().Assignments {
		if assignment.ID == id && assignment.RunID == params.RunID {
			found = true
			break
		}
	}
	cancelled := false
	if found {
		cancelled = r.workerPool.Cancel(context.Background(), id, params.Reason)
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.WorkerAssignmentCancelResult{
		Accepted: true, RunID: params.RunID, AssignmentID: params.AssignmentID, Cancelled: cancelled,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleWorkerPoolStatus(req jsonrpc.Request) error {
	snapshot := r.workerPool.Snapshot()
	result := methods.WorkerPoolStatusResult{Pool: methodPoolSnapshot(snapshot)}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func methodPoolSnapshot(snapshot worker.PoolSnapshot) methods.PoolSnapshot {
	result := methods.PoolSnapshot{
		Configured: snapshot.Configured, Ready: snapshot.Ready, Busy: snapshot.Busy,
		Draining: snapshot.Draining, Unhealthy: snapshot.Unhealthy, Stopped: snapshot.Stopped,
		Queued: snapshot.Queued, Running: snapshot.Running, WaitingPermission: snapshot.WaitingPermission,
		Paused:  snapshot.Paused,
		Workers: []methods.WorkerRef{}, Assignments: []methods.AssignmentRecord{},
	}
	for _, item := range snapshot.Workers {
		result.Workers = append(result.Workers, methodWorkerRef(item))
	}
	for _, item := range snapshot.Assignments {
		result.Assignments = append(result.Assignments, methodAssignmentRecord(item))
	}
	return result
}

func methodWorkerRef(item worker.WorkerSnapshot) methods.WorkerRef {
	return methods.WorkerRef{
		ID: string(item.ID), State: methods.WorkerState(item.State),
		CurrentAssignmentID: string(item.CurrentAssignmentID), ProfileKey: item.ProfileKey,
		MailboxDepth: item.MailboxDepth, MailboxCapacity: item.MailboxCapacity, Healthy: item.Healthy,
	}
}

func methodAssignmentRecord(item worker.AssignmentSnapshot) methods.AssignmentRecord {
	return methods.AssignmentRecord{
		ID: string(item.ID), RunID: item.RunID, WorkerID: string(item.WorkerID),
		OriginWorkerID: string(item.OriginWorkerID), ProfileKey: item.ProfileKey, Task: item.Task,
		Status: methods.AssignmentStatus(item.Status), Result: item.Result, Error: item.Error,
		ExecutionStats: methodWorkerExecutionStats(item.Stats),
		CreatedAt:      item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
	}
}

func methodWorkerExecutionStats(stats worker.ExecutionStats) methods.WorkerExecutionStats {
	return methods.WorkerExecutionStats{
		MaxTurns: stats.MaxTurns, LoopTurns: stats.LoopTurns, ProviderRequests: stats.ProviderRequests,
		ToolCallsRequested: stats.ToolCallsRequested, ToolCallsExecuted: stats.ToolCallsExecuted,
		ToolCallBudget: stats.ToolCallBudget, MaxTurnsReached: stats.MaxTurnsReached,
		ToolBudgetReached: stats.ToolBudgetReached,
	}
}
