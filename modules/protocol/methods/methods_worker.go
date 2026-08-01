package methods

// methods_worker.go — Worker / Assignment / Message DTOs.
//
// Relocated from methods.go by domain (docs/plans/2026-07-19-convergence-wave.md
// Wave C Task C3). Method-name constants remain in methods.go.

import (
	"encoding/json"
	"time"
)

type WorkerState string

const (
	WorkerStateReady     WorkerState = "ready"
	WorkerStateBusy      WorkerState = "busy"
	WorkerStateDraining  WorkerState = "draining"
	WorkerStateUnhealthy WorkerState = "unhealthy"
	WorkerStateStopped   WorkerState = "stopped"
)

type AssignmentStatus string

const (
	AssignmentStatusQueued            AssignmentStatus = "queued"
	AssignmentStatusRunning           AssignmentStatus = "running"
	AssignmentStatusPaused            AssignmentStatus = "paused"
	AssignmentStatusWaitingPermission AssignmentStatus = "waiting_permission"
	AssignmentStatusCompleted         AssignmentStatus = "completed"
	AssignmentStatusFailed            AssignmentStatus = "failed"
	AssignmentStatusCancelled         AssignmentStatus = "cancelled"
)

type MessageKind string

const (
	MessageKindRequest MessageKind = "request"
	MessageKindUpdate  MessageKind = "update"
	MessageKindResult  MessageKind = "result"
	MessageKindControl MessageKind = "control"
)

// WorkerRef describes a stable Worker slot. ProfileKey is assignment-scoped;
// it is empty while the Worker is idle.
type WorkerRef struct {
	ID                  string      `json:"id"`
	State               WorkerState `json:"state,omitempty"`
	CurrentAssignmentID string      `json:"current_assignment_id,omitempty"`
	ProfileKey          string      `json:"profile_key,omitempty"`
	MailboxDepth        int         `json:"mailbox_depth,omitempty"`
	MailboxCapacity     int         `json:"mailbox_capacity,omitempty"`
	Healthy             bool        `json:"healthy"`
}

type AssignmentRecord struct {
	ID             string               `json:"id"`
	RunID          string               `json:"run_id"`
	WorkerID       string               `json:"worker_id"`
	OriginWorkerID string               `json:"origin_worker_id,omitempty"`
	ProfileKey     string               `json:"profile_key,omitempty"`
	Task           string               `json:"task"`
	Status         AssignmentStatus     `json:"status"`
	Result         string               `json:"result,omitempty"`
	Error          string               `json:"error,omitempty"`
	ExecutionStats WorkerExecutionStats `json:"execution_stats,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
	StartedAt      *time.Time           `json:"started_at,omitempty"`
	FinishedAt     *time.Time           `json:"finished_at,omitempty"`
}

type WorkerExecutionStats struct {
	MaxTurns           int  `json:"max_turns,omitempty"`
	LoopTurns          int  `json:"loop_turns,omitempty"`
	ProviderRequests   int  `json:"provider_requests,omitempty"`
	ToolCallsRequested int  `json:"tool_calls_requested,omitempty"`
	ToolCallsExecuted  int  `json:"tool_calls_executed,omitempty"`
	ToolCallBudget     int  `json:"tool_call_budget,omitempty"`
	MaxTurnsReached    bool `json:"max_turns_reached,omitempty"`
	ToolBudgetReached  bool `json:"tool_budget_reached,omitempty"`
}

type PoolSnapshot struct {
	Configured        int                `json:"configured"`
	Ready             int                `json:"ready"`
	Busy              int                `json:"busy"`
	Draining          int                `json:"draining"`
	Unhealthy         int                `json:"unhealthy"`
	Stopped           int                `json:"stopped"`
	Queued            int                `json:"queued"`
	Running           int                `json:"running"`
	WaitingPermission int                `json:"waiting_permission"`
	Paused            int                `json:"paused"`
	Workers           []WorkerRef        `json:"workers"`
	Assignments       []AssignmentRecord `json:"assignments"`
}

type WorkerMessage struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	FromWorkerID     string          `json:"from_worker_id"`
	FromAssignmentID string          `json:"from_assignment_id"`
	ToWorkerID       string          `json:"to_worker_id"`
	ToAssignmentID   string          `json:"to_assignment_id"`
	Kind             MessageKind     `json:"kind"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	ReplyTo          string          `json:"reply_to,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
}
