// RunService types, lifecycle queries, Start/Cancel entrypoints, and runtime proxies.
// Start pipeline helpers: run_start.go; events: run_events.go (docs/41 W5-4).

package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	protows "redpanda/protocol/ws"
)

const (
	defaultMaxConcurrentRuns = 3
	maxConcurrentRunsCap     = 16
	// defaultRuntimeMode isolates multi-session concurrent runs in dedicated agent processes.
	defaultRuntimeMode = "per_run_process"
)

type RunService struct {
	repos   repository.Set
	hub     *eventhub.Hub
	runtime *runtimeclient.Client
	// packer assembles the model-facing conversation for a run (docs/48 Wave C,
	// docs/plans/2026-07-19-convergence-wave.md Wave B Task B1). Shared instance
	// supplied by Set so Run and Session stay decoupled from packing internals.
	packer *SessionContextPacker
	// startMu serializes admission so concurrent budget checks and slot reservation are atomic.
	startMu *sync.Mutex
	// attachments resolves workspace path / upload refs during run.start admission
	// (docs/51 §6.2). Wired by Set after construction.
	attachments *AttachmentService
}

// AttachAttachmentService wires the attachment resolver. Called by Set once
// both services exist (avoids an init-order cycle).
func (r *RunService) AttachAttachmentService(s *AttachmentService) { r.attachments = s }

type StartRunResult struct {
	RunID       string `json:"run_id"`
	SessionID   string `json:"session_id"`
	Accepted    bool   `json:"accepted"`
	Subscribed  bool   `json:"subscribed"`
	RuntimeMode string `json:"runtime_mode"`
	RunSequence uint64 `json:"run_seq"`
}

type runAdmission struct {
	runID       string
	session     model.Session
	inputText   string
	runtimeMode string
	attachments []resolvedAttachment // validated image refs (docs/51 §6.2)
}

type RunRecordDTO struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	RuntimeMode   string     `json:"runtime_mode"`
	Status        string     `json:"status"`
	Input         string     `json:"input,omitempty"`
	LastEventType string     `json:"last_event_type,omitempty"`
	LastRunSeq    uint64     `json:"last_run_seq"`
	MessageCount  int        `json:"message_count"`
	ToolCount     int        `json:"tool_count"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type RunEventDTO struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	RunID        string         `json:"run_id"`
	SessionID    string         `json:"session_id"`
	AssignmentID string         `json:"assignment_id"`
	RunSeq       uint64         `json:"run_seq"`
	WorkerSeq    uint64         `json:"worker_seq"`
	WorkerID     string         `json:"worker_id"`
	ProfileKey   string         `json:"profile_key,omitempty"`
	StreamKind   string         `json:"stream_kind,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

func NewRunService(repos repository.Set, hub *eventhub.Hub, runtime *runtimeclient.Client) RunService {
	return NewRunServiceWithPacker(repos, hub, runtime, NewSessionContextPacker(repos))
}

// NewRunServiceWithPacker constructs a RunService with an explicit context
// packer. Used by Set to share a single Packer instance across services
// (docs/plans/2026-07-19-convergence-wave.md Wave B Task B1).
func NewRunServiceWithPacker(repos repository.Set, hub *eventhub.Hub, runtime *runtimeclient.Client, packer *SessionContextPacker) RunService {
	service := RunService{repos: repos, hub: hub, runtime: runtime, packer: packer, startMu: &sync.Mutex{}}
	if runtime != nil {
		runtime.SetRunExitHandler(service.HandleRuntimeExit)
	}
	return service
}

// RecoverStaleRunsOnStartup marks any leftover "running"/"waiting_permission" runs as failed.
// Call this exactly once during gateway boot to release the concurrency budget after crashes/restarts.
func (r RunService) RecoverStaleRunsOnStartup() (int64, error) {
	return r.repos.Runs.RecoverStaleRuns()
}

func (r RunService) RuntimeStatus() map[string]any {
	status := map[string]any{
		"available":            false,
		"mode":                 defaultRuntimeMode,
		"default_runtime_mode": defaultRuntimeMode,
		"max_concurrent_runs":  defaultMaxConcurrentRuns,
		"active_runs":          int64(0),
		"isolation":            "per_run_process",
	}
	if active, err := r.repos.Runs.CountActive(); err == nil {
		status["active_runs"] = active
	}
	if r.runtime == nil {
		status["reason"] = "runtime client not configured"
		return status
	}
	for key, value := range r.runtime.Status() {
		status[key] = value
	}
	// Prefer explicit default for clients that only look at mode when idle.
	if _, ok := status["default_runtime_mode"]; !ok {
		status["default_runtime_mode"] = defaultRuntimeMode
	}
	return status
}

// resolveMaxConcurrentRuns prefers per-run option, then env, then default.
func resolveMaxConcurrentRuns(options map[string]any) int {
	if n := intOption(options, "max_concurrent_runs"); n > 0 {
		if n > maxConcurrentRunsCap {
			return maxConcurrentRunsCap
		}
		return n
	}
	if env := strings.TrimSpace(os.Getenv("RED_PANDA_MAX_CONCURRENT_RUNS")); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			if parsed > maxConcurrentRunsCap {
				return maxConcurrentRunsCap
			}
			return parsed
		}
	}
	return defaultMaxConcurrentRuns
}

func (r RunService) Get(runID string) (RunRecordDTO, error) {
	row, err := r.repos.Runs.Get(runID)
	if err != nil {
		return RunRecordDTO{}, err
	}
	return runRecordDTO(row), nil
}

func (r RunService) ListBySession(sessionID string, limit int) ([]RunRecordDTO, error) {
	rows, err := r.repos.Runs.ListBySession(sessionID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunRecordDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, runRecordDTO(row))
	}
	return items, nil
}

func (r RunService) Events(rootRunID string, afterSeq uint64, limit int) ([]RunEventDTO, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	items, err := r.repos.RunEvents.ListAfter(rootRunID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	dtos := make([]RunEventDTO, 0, len(items))
	for _, event := range items {
		dtos = append(dtos, runEventDTO(event))
	}
	return dtos, nil
}

func (r RunService) Start(ctx context.Context, payload protows.RunStartPayload) (result StartRunResult, err error) {
	if r.runtime == nil {
		return StartRunResult{}, fmt.Errorf("runtime client not configured")
	}

	admission, err := r.admitRun(payload)
	if err != nil {
		return StartRunResult{}, err
	}

	defer func() {
		if err == nil {
			return
		}
		_ = r.repos.Runs.Finish(admission.runID, "failed", err.Error())
	}()

	params, err := r.prepareRun(admission, payload)
	if err != nil {
		return StartRunResult{}, err
	}

	result, err = r.dispatchRun(ctx, admission, params, payload.Subscribe)
	return result, err
}

func (r RunService) Cancel(ctx context.Context, runID string, reason string) error {
	if r.runtime == nil {
		return fmt.Errorf("runtime client not configured")
	}
	_, err := r.runtime.CancelRun(ctx, methods.RunCancelParams{RunID: runID, Reason: reason})
	return err
}

func (r RunService) Workers(ctx context.Context, params methods.WorkerListParams) (methods.WorkerListResult, error) {
	if r.runtime == nil {
		return methods.WorkerListResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.Workers(ctx, params)
}

func (r RunService) CancelAssignment(ctx context.Context, params methods.WorkerAssignmentCancelParams) (methods.WorkerAssignmentCancelResult, error) {
	if r.runtime == nil {
		return methods.WorkerAssignmentCancelResult{}, fmt.Errorf("runtime client not configured")
	}
	return r.runtime.CancelAssignment(ctx, params)
}

func (r RunService) ResolvePermission(ctx context.Context, params permission.ResolveParams) (permission.ResolveResult, error) {
	if r.runtime == nil {
		return permission.ResolveResult{}, fmt.Errorf("runtime client not configured")
	}
	result, err := r.runtime.ResolvePermission(ctx, params)
	if err != nil {
		return permission.ResolveResult{}, err
	}
	_ = r.repos.Permissions.Resolve(params)
	return result, nil
}

func (r RunService) Subscribe(runID string) (<-chan events.EnvelopeV2, func()) {
	return r.hub.Subscribe(runID)
}

func (r RunService) Replay(runID string, afterSeq uint64) ([]events.EnvelopeV2, error) {
	return r.repos.RunEvents.ListAfter(runID, afterSeq, 500)
}
