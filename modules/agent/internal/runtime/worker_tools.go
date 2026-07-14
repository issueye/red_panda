package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// marshalToolJSON is a local helper to return compact JSON tool results.
func marshalToolJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var delegatedWorkerDenylist = []string{
	"worker.delegate",
}

const (
	internalWorkerSendMethod    = "internal.worker.send"
	internalWorkerReceiveMethod = "internal.worker.receive"
	workerDelegateMaxAttempts   = 2
)

func (r *Runtime) executeWorkerDelegate(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	if runCtx.Reply == nil {
		return "", fmt.Errorf("reply context is required")
	}
	task := strings.TrimSpace(agenttools.StringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("task is required")
	}
	profileKey := strings.TrimSpace(agenttools.StringArg(call.Arguments, "profile_key"))
	maxTurns := agenttools.IntArg(call.Arguments, "max_turns", 0)
	child := delegatedWorkerReply(*runCtx.Reply, task, profileKey, maxTurns)
	if specialist, ok := resolveGoalSpecialist(runCtx.Reply.Options.WorkerProfiles, profileKey); ok {
		if err := r.validateGoalSpecialistPhase(runCtx.RunID, specialist); err != nil {
			return "", err
		}
		goalID := strings.TrimSpace(runCtx.Reply.Options.GoalID)
		objective := ""
		if goal := runCtx.Reply.Options.GoalContext; goal != nil {
			objective = goal.Objective
			if goalID == "" {
				goalID = strings.TrimSpace(goal.GoalID)
			}
		}
		child.Options.MaxToolTurns = r.applyGoalSpecialist(&child, specialist, task, child.Options.MaxToolTurns, runCtx.RunID, runCtx.SessionID, goalID, objective)
	}
	assignment, result, err := r.executeDelegatedAssignment(ctx, runCtx, workerExecutionSpec{
		Kind:       workerExecutionDelegated,
		Params:     child,
		Parent:     *runCtx.Reply,
		ProfileKey: profileKey,
		Task:       task,
		MaxTurns:   child.Options.MaxToolTurns,
	})
	if err != nil {
		return "", err
	}
	return marshalToolJSON(map[string]any{
		"assignment_id": assignment.AssignmentID,
		"worker_id":     assignment.WorkerID,
		"status":        result.Status,
		"output":        strings.TrimSpace(agenttools.TruncateToolOutput(result.Output)),
	})
}

func (r *Runtime) executeDelegatedAssignment(ctx context.Context, runCtx agenttools.ToolRunContext, spec workerExecutionSpec) (worker.AssignmentRef, worker.AssignmentResult, error) {
	callerID := worker.AssignmentID(strings.TrimSpace(runCtx.AssignmentID))
	if callerID == "" {
		return worker.AssignmentRef{}, worker.AssignmentResult{}, worker.ErrAssignmentNotFound
	}
	var lastAssignment worker.AssignmentRef
	var lastResult worker.AssignmentResult
	for attempt := 1; attempt <= workerDelegateMaxAttempts; attempt++ {
		executionCtx := withWorkerExecution(ctx, spec)
		assignment, err := r.workerPool.Delegate(executionCtx, callerID, worker.SubmitRequest{
			RunID: runCtx.RunID, ProfileKey: spec.ProfileKey, Task: spec.Task,
			Attempt: attempt, RetryOf: lastAssignment.AssignmentID,
		})
		if err != nil {
			if ctx.Err() != nil || attempt == workerDelegateMaxAttempts {
				return lastAssignment, lastResult, err
			}
			if retryErr := waitWorkerRetry(ctx, attempt); retryErr != nil {
				return lastAssignment, lastResult, retryErr
			}
			continue
		}
		lastAssignment = assignment
		delegatedCtx := withAssignment(executionCtx, assignment.AssignmentID, assignment.WorkerID)
		emitAssignment := func(status worker.AssignmentStatus, result worker.AssignmentResult, errText string, retrying bool) {
			workerState, currentID := "ready", ""
			if status == worker.AssignmentQueued || status == worker.AssignmentRunning {
				workerState, currentID = "busy", string(assignment.AssignmentID)
			}
			_ = r.emitAgentEvent(delegatedCtx, *runCtx.Reply, events.AgentRef{Name: spec.ProfileKey}, events.EventWorkerAssignmentUpdated, nil, map[string]any{
				"status": string(status), "task": spec.Task, "profile_key": spec.ProfileKey,
				"attempt": assignment.Attempt, "retry_of": string(assignment.RetryOf),
				"retrying": retrying, "retry_in_ms": retryDelay(attempt).Milliseconds(),
				"result": strings.TrimSpace(result.Output), "error": errText,
				"worker": map[string]any{"id": string(assignment.WorkerID), "state": workerState,
					"profile_key": spec.ProfileKey, "current_assignment_id": currentID},
			})
		}
		emitAssignment(worker.AssignmentQueued, worker.AssignmentResult{}, "", false)
		emitAssignment(worker.AssignmentRunning, worker.AssignmentResult{}, "", false)

		result, waitErr := r.workerPool.Wait(ctx, assignment.AssignmentID)
		lastResult = result
		finalStatus := result.Status
		if finalStatus == "" {
			if waitErr != nil {
				finalStatus = worker.AssignmentFailed
			} else {
				finalStatus = worker.AssignmentCompleted
			}
		}
		if finalStatus != worker.AssignmentCompleted && result.Error == "" {
			result.Error = firstWorkerError(waitErr, "worker assignment did not complete")
		}
		lastResult = result
		retrying := finalStatus != worker.AssignmentCompleted && ctx.Err() == nil && attempt < workerDelegateMaxAttempts
		emitAssignment(finalStatus, result, result.Error, retrying)
		if !retrying {
			if finalStatus != worker.AssignmentCompleted {
				return assignment, result, errors.New(result.Error)
			}
			return assignment, result, nil
		}
		if retryErr := waitWorkerRetry(ctx, attempt); retryErr != nil {
			return assignment, result, retryErr
		}
	}
	return lastAssignment, lastResult, errors.New("worker assignment retry limit reached")
}

func retryDelay(attempt int) time.Duration { return time.Duration(attempt) * 500 * time.Millisecond }

func waitWorkerRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(retryDelay(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstWorkerError(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func delegatedWorkerReply(parent methods.ReplyParams, task, profileKey string, maxTurns int) methods.ReplyParams {
	child := parent
	profile, hasProfile := workerProfile(parent.Options.WorkerProfiles, profileKey)
	child.Session.Conversation = nil
	child.Input.Text = task
	child.Options.MemoryContext = nil
	child.Options.TodoContext = nil
	disableGoalPipelineForChild(&child.Options)
	if hasProfile {
		child.Options.ProviderProfileID = profile.ProviderProfileID
		child.Options.ProviderName = firstNonEmpty(profile.ProviderName, child.Options.ProviderName)
		child.Options.Model = firstNonEmpty(profile.Model, child.Options.Model)
		child.Options.ToolPolicy = firstNonEmpty(profile.ToolPolicy, child.Options.ToolPolicy)
		if len(profile.ToolAllowlist) > 0 {
			child.Options.ToolAllowlist = append([]string(nil), profile.ToolAllowlist...)
			child.Options.ToolAllowlist = appendUniqueStrings(child.Options.ToolAllowlist, "worker.send", "worker.receive")
		}
		child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, profile.ToolDenylist...)
	}
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, worker.DelegatedDenylist...)
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, delegatedWorkerDenylist...)
	if maxTurns <= 0 && hasProfile && profile.DefaultMaxTurns > 0 {
		maxTurns = profile.DefaultMaxTurns
	}
	if maxTurns <= 0 {
		maxTurns = worker.DefaultToolTurns
	}
	child.Options.MaxToolTurns = maxTurns
	role := "You are a delegated Worker. Complete only the assigned task and return one final report. You cannot delegate or start legacy Workers."
	if hasProfile && strings.TrimSpace(profile.SystemPrompt) != "" {
		role = strings.TrimSpace(profile.SystemPrompt) + "\n\n" + role
	}
	if profileKey != "" {
		role += " Worker Profile: " + profileKey + "."
	}
	child.Options.SpecialistContext = &methods.SpecialistContext{Kind: "delegated_worker_proxy", Context: role}
	return child
}

func workerProfile(profiles []methods.WorkerProfileRef, profileKey string) (methods.WorkerProfileRef, bool) {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return methods.WorkerProfileRef{}, false
	}
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Key), profileKey) && profile.Enabled {
			return profile, true
		}
	}
	return methods.WorkerProfileRef{}, false
}

func (r *Runtime) executeWorkerList(_ context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	runID := strings.TrimSpace(runCtx.RunID)
	workerID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "worker_id"))
	assignmentID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "assignment_id"))
	snapshot := workerSnapshotForRun(r.workerPool.Snapshot(), runID)
	workers := snapshot.Workers[:0]
	for _, item := range snapshot.Workers {
		if workerID == "" || string(item.ID) == workerID {
			workers = append(workers, item)
		}
	}
	assignments := snapshot.Assignments[:0]
	for _, item := range snapshot.Assignments {
		if assignmentID != "" && string(item.ID) != assignmentID {
			continue
		}
		if workerID != "" && string(item.WorkerID) != workerID {
			continue
		}
		assignments = append(assignments, item)
	}
	return marshalToolJSON(map[string]any{"workers": workers, "assignments": assignments})
}

func (r *Runtime) executeWorkerCancel(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	id := worker.AssignmentID(strings.TrimSpace(agenttools.StringArg(call.Arguments, "assignment_id")))
	if id == "" {
		return "", fmt.Errorf("assignment_id is required")
	}
	belongsToRun := false
	for _, assignment := range r.workerPool.Snapshot().Assignments {
		if assignment.ID == id && assignment.RunID == runCtx.RunID {
			belongsToRun = true
			break
		}
	}
	if !belongsToRun {
		return "", worker.ErrAssignmentNotFound
	}
	reason := strings.TrimSpace(agenttools.StringArg(call.Arguments, "reason"))
	if reason == "" {
		reason = "cancelled by Worker"
	}
	cancelled := r.workerPool.Cancel(ctx, id, reason)
	return marshalToolJSON(map[string]any{
		"assignment_id": id,
		"run_id":        runCtx.RunID,
		"cancelled":     cancelled,
	})
}

func (r *Runtime) executeWorkerPoolStatus(_ context.Context, runCtx agenttools.ToolRunContext, _ tools.Call) (string, error) {
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	snapshot := workerSnapshotForRun(r.workerPool.Snapshot(), strings.TrimSpace(runCtx.RunID))
	return marshalToolJSON(map[string]any{"pool": snapshot})
}

// workerSnapshotForRun preserves global capacity/health counters while
// removing assignment-scoped data owned by other runs.
func workerSnapshotForRun(snapshot worker.PoolSnapshot, runID string) worker.PoolSnapshot {
	visibleAssignments := make(map[worker.AssignmentID]struct{})
	assignments := make([]worker.AssignmentSnapshot, 0, len(snapshot.Assignments))
	for _, assignment := range snapshot.Assignments {
		if runID == "" || assignment.RunID != runID {
			continue
		}
		visibleAssignments[assignment.ID] = struct{}{}
		assignments = append(assignments, assignment)
	}
	snapshot.Assignments = assignments
	workers := append([]worker.WorkerSnapshot(nil), snapshot.Workers...)
	for i := range workers {
		if _, visible := visibleAssignments[workers[i].CurrentAssignmentID]; !visible {
			workers[i].CurrentAssignmentID = ""
			workers[i].ProfileKey = ""
		}
	}
	snapshot.Workers = workers
	return snapshot
}

func (r *Runtime) executeWorkerSend(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if delegatedWorkerProxy(runCtx) {
		proxy := runCtx.Reply.Options.WorkerContext
		raw, err := r.callGateway(ctx, internalWorkerSendMethod, map[string]any{
			"to_worker_id":     agenttools.StringArg(call.Arguments, "to_worker_id"),
			"to_assignment_id": agenttools.StringArg(call.Arguments, "to_assignment_id"),
			"kind":             agenttools.StringArg(call.Arguments, "kind"),
			"payload":          call.Arguments["payload"],
			"worker_context":   proxy,
		})
		return string(raw), err
	}
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	if !trustedWorkerContext(r.workerPool.Snapshot(), runCtx) {
		return "", worker.ErrAssignmentNotActive
	}
	toWorkerID := worker.WorkerID(strings.TrimSpace(agenttools.StringArg(call.Arguments, "to_worker_id")))
	if toWorkerID == "" {
		return "", fmt.Errorf("to_worker_id is required")
	}
	toAssignmentID := worker.AssignmentID(strings.TrimSpace(agenttools.StringArg(call.Arguments, "to_assignment_id")))
	if toAssignmentID != "" && !activeWorkerAssignment(r.workerPool.Snapshot(), runCtx.RunID, toWorkerID, toAssignmentID) {
		return "", worker.ErrAssignmentNotActive
	}
	kind := worker.MessageKind(strings.TrimSpace(agenttools.StringArg(call.Arguments, "kind")))
	payloadValue, ok := call.Arguments["payload"]
	if !ok {
		return "", fmt.Errorf("payload is required")
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return "", fmt.Errorf("marshal Worker message payload: %w", err)
	}
	messageID := "message-" + strings.TrimSpace(call.ID)
	if messageID == "message-" {
		messageID = fmt.Sprintf("message-%d", time.Now().UnixNano())
	}
	message := worker.WorkerMessage{
		ID:               messageID,
		RunID:            runCtx.RunID,
		FromWorkerID:     worker.WorkerID(runCtx.WorkerID),
		FromAssignmentID: worker.AssignmentID(runCtx.AssignmentID),
		ToWorkerID:       toWorkerID,
		ToAssignmentID:   toAssignmentID,
		Kind:             kind,
		Payload:          payload,
	}
	if err := r.workerPool.Send(ctx, message); err != nil {
		return "", err
	}
	return marshalToolJSON(map[string]any{
		"accepted":         true,
		"message_id":       messageID,
		"to_worker_id":     toWorkerID,
		"to_assignment_id": toAssignmentID,
	})
}

func (r *Runtime) executeWorkerReceive(ctx context.Context, runCtx agenttools.ToolRunContext, _ tools.Call) (string, error) {
	if delegatedWorkerProxy(runCtx) {
		raw, err := r.callGateway(ctx, internalWorkerReceiveMethod, map[string]any{
			"worker_context": runCtx.Reply.Options.WorkerContext,
		})
		return string(raw), err
	}
	if r.workerPool == nil {
		return "", fmt.Errorf("WorkerPool is not available")
	}
	if !trustedWorkerContext(r.workerPool.Snapshot(), runCtx) {
		return "", worker.ErrAssignmentNotActive
	}
	message, err := r.workerPool.Receive(ctx, worker.AssignmentID(runCtx.AssignmentID))
	if err != nil {
		return "", err
	}
	return marshalToolJSON(message)
}

func delegatedWorkerProxy(runCtx agenttools.ToolRunContext) bool {
	return runCtx.Reply != nil && runCtx.Reply.Options.WorkerContext != nil &&
		runCtx.Reply.Options.WorkerContext.ProxyMessages
}

func trustedWorkerContext(snapshot worker.PoolSnapshot, runCtx agenttools.ToolRunContext) bool {
	assignmentID := worker.AssignmentID(strings.TrimSpace(runCtx.AssignmentID))
	workerID := worker.WorkerID(strings.TrimSpace(runCtx.WorkerID))
	if assignmentID == "" || workerID == "" || strings.TrimSpace(runCtx.RunID) == "" {
		return false
	}
	return activeWorkerAssignment(snapshot, runCtx.RunID, workerID, assignmentID)
}

func activeWorkerAssignment(snapshot worker.PoolSnapshot, runID string, workerID worker.WorkerID, assignmentID worker.AssignmentID) bool {
	assignmentActive := false
	for _, assignment := range snapshot.Assignments {
		if assignment.ID == assignmentID && assignment.WorkerID == workerID && assignment.RunID == runID && !assignment.Status.Terminal() {
			assignmentActive = true
			break
		}
	}
	if !assignmentActive {
		return false
	}
	for _, slot := range snapshot.Workers {
		if slot.ID == workerID {
			return slot.CurrentAssignmentID == assignmentID
		}
	}
	return false
}
