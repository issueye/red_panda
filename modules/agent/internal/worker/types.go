package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultPoolSize        = 8
	MaxPoolSize            = 8
	DefaultMailboxCapacity = 64
	DefaultMaxMessageBytes = 64 << 10
	DefaultMessageTTL      = 5 * time.Minute
)

var (
	ErrInvalidConfig       = errors.New("worker: invalid pool configuration")
	ErrPoolClosed          = errors.New("worker: pool closed")
	ErrCapacityExhausted   = errors.New("worker: capacity exhausted")
	ErrAssignmentNotFound  = errors.New("worker: assignment not found")
	ErrAssignmentNotActive = errors.New("worker: assignment is not active")
	ErrNestedDelegation    = errors.New("worker: nested delegation denied")
	ErrCallerRunMismatch   = errors.New("worker: caller assignment belongs to another run")
	ErrWorkerNotFound      = errors.New("worker: worker not found")
	ErrMailboxFull         = errors.New("worker: mailbox full")
	ErrMailboxClosed       = errors.New("worker: mailbox closed")
	ErrMessageTooLarge     = errors.New("worker: message too large")
	ErrMessageExpired      = errors.New("worker: message expired")
	ErrCrossRunMessage     = errors.New("worker: cross-run message denied")
	ErrInvalidMessage      = errors.New("worker: invalid message")
)

type WorkerID string
type AssignmentID string

type WorkerState string

const (
	WorkerReady     WorkerState = "ready"
	WorkerBusy      WorkerState = "busy"
	WorkerDraining  WorkerState = "draining"
	WorkerUnhealthy WorkerState = "unhealthy"
	WorkerStopped   WorkerState = "stopped"
)

type AssignmentStatus string

const (
	AssignmentQueued            AssignmentStatus = "queued"
	AssignmentRunning           AssignmentStatus = "running"
	AssignmentPaused            AssignmentStatus = "paused"
	AssignmentWaitingPermission AssignmentStatus = "waiting_permission"
	AssignmentCompleted         AssignmentStatus = "completed"
	AssignmentFailed            AssignmentStatus = "failed"
	AssignmentCancelled         AssignmentStatus = "cancelled"
)

func (s AssignmentStatus) Terminal() bool {
	return s == AssignmentCompleted || s == AssignmentFailed || s == AssignmentCancelled
}

type Config struct {
	Size            int
	MailboxCapacity int
	MaxMessageBytes int
	MessageTTL      time.Duration
}

type Event struct {
	Type    string
	Payload json.RawMessage
}

type EventSink func(Event)

type ExecuteRequest struct {
	AssignmentID AssignmentID
	RunID        string
	WorkerID     WorkerID
	ProfileKey   string
	Task         string
}

type ExecuteResult struct {
	Output string
	Stats  ExecutionStats
}

type ExecutionStats struct {
	MaxTurns           int  `json:"max_turns,omitempty"`
	LoopTurns          int  `json:"loop_turns,omitempty"`
	ProviderRequests   int  `json:"provider_requests,omitempty"`
	ToolCallsRequested int  `json:"tool_calls_requested,omitempty"`
	ToolCallsExecuted  int  `json:"tool_calls_executed,omitempty"`
	ToolCallBudget     int  `json:"tool_call_budget,omitempty"`
	MaxTurnsReached    bool `json:"max_turns_reached,omitempty"`
	ToolBudgetReached  bool `json:"tool_budget_reached,omitempty"`
}

// Executor owns the reusable execution resource attached to one Worker slot.
type Executor interface {
	Execute(context.Context, ExecuteRequest, EventSink) (ExecuteResult, error)
	Cancel(context.Context, AssignmentID, string) error
	Reset(context.Context) error
	Close(context.Context) error
	Healthy() bool
}

// PausableExecutor retains an active execution resource while suspended.
type PausableExecutor interface {
	Pause(context.Context, AssignmentID, string) error
	Resume(context.Context, AssignmentID) error
}

type ExecutorFactory func(WorkerID) (Executor, error)

type Worker struct {
	ID                  WorkerID
	state               WorkerState
	currentAssignmentID AssignmentID
	mailbox             *Mailbox
	executor            Executor
}

type Assignment struct {
	ID             AssignmentID
	RunID          string
	WorkerID       WorkerID
	OriginWorkerID WorkerID
	ProfileKey     string
	Task           string
	Attempt        int
	RetryOf        AssignmentID
	Status         AssignmentStatus
	Result         string
	Error          string
	Stats          ExecutionStats
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	resumeStatus   AssignmentStatus

	cancel     context.CancelFunc
	cancelDone chan struct{}
	done       chan struct{}
	settled    chan struct{}
}

type SubmitRequest struct {
	RunID      string
	ProfileKey string
	Task       string
	Attempt    int
	RetryOf    AssignmentID
	EventSink  EventSink
}

type AssignmentRef struct {
	AssignmentID   AssignmentID
	WorkerID       WorkerID
	OriginWorkerID WorkerID
	Attempt        int
	RetryOf        AssignmentID
}

type AssignmentResult struct {
	AssignmentID AssignmentID
	WorkerID     WorkerID
	Attempt      int
	RetryOf      AssignmentID
	Status       AssignmentStatus
	Output       string
	Error        string
	Stats        ExecutionStats
}

type WorkerSnapshot struct {
	ID                  WorkerID     `json:"id"`
	State               WorkerState  `json:"state"`
	CurrentAssignmentID AssignmentID `json:"current_assignment_id,omitempty"`
	ProfileKey          string       `json:"profile_key,omitempty"`
	MailboxDepth        int          `json:"mailbox_depth"`
	MailboxCapacity     int          `json:"mailbox_capacity"`
	Healthy             bool         `json:"healthy"`
}

type AssignmentSnapshot struct {
	ID             AssignmentID     `json:"id"`
	RunID          string           `json:"run_id"`
	WorkerID       WorkerID         `json:"worker_id"`
	OriginWorkerID WorkerID         `json:"origin_worker_id,omitempty"`
	ProfileKey     string           `json:"profile_key,omitempty"`
	Task           string           `json:"task"`
	Attempt        int              `json:"attempt,omitempty"`
	RetryOf        AssignmentID     `json:"retry_of,omitempty"`
	Status         AssignmentStatus `json:"status"`
	Result         string           `json:"result,omitempty"`
	Error          string           `json:"error,omitempty"`
	Stats          ExecutionStats   `json:"execution_stats,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	FinishedAt     *time.Time       `json:"finished_at,omitempty"`
}

type PoolSnapshot struct {
	Configured        int                  `json:"configured"`
	Ready             int                  `json:"ready"`
	Busy              int                  `json:"busy"`
	Draining          int                  `json:"draining"`
	Unhealthy         int                  `json:"unhealthy"`
	Stopped           int                  `json:"stopped"`
	Queued            int                  `json:"queued"`
	Running           int                  `json:"running"`
	WaitingPermission int                  `json:"waiting_permission"`
	Paused            int                  `json:"paused"`
	Workers           []WorkerSnapshot     `json:"workers"`
	Assignments       []AssignmentSnapshot `json:"assignments"`
}

type MessageKind string

const (
	MessageRequest MessageKind = "request"
	MessageUpdate  MessageKind = "update"
	MessageResult  MessageKind = "result"
	MessageControl MessageKind = "control"
)

type WorkerMessage struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	FromWorkerID     WorkerID        `json:"from_worker_id"`
	ToWorkerID       WorkerID        `json:"to_worker_id"`
	FromAssignmentID AssignmentID    `json:"from_assignment_id"`
	ToAssignmentID   AssignmentID    `json:"to_assignment_id"`
	Kind             MessageKind     `json:"kind"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	ReplyTo          string          `json:"reply_to,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
}
