package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	DefaultPoolSize        = 2
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
}

// Executor owns the reusable execution resource attached to one Worker slot.
type Executor interface {
	Execute(context.Context, ExecuteRequest, EventSink) (ExecuteResult, error)
	Cancel(context.Context, AssignmentID, string) error
	Reset(context.Context) error
	Close(context.Context) error
	Healthy() bool
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
	Status         AssignmentStatus
	Result         string
	Error          string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time

	cancel     context.CancelFunc
	cancelDone chan struct{}
	done       chan struct{}
	settled    chan struct{}
}

type SubmitRequest struct {
	RunID      string
	ProfileKey string
	Task       string
	EventSink  EventSink
}

type AssignmentRef struct {
	AssignmentID   AssignmentID
	WorkerID       WorkerID
	OriginWorkerID WorkerID
}

type AssignmentResult struct {
	AssignmentID AssignmentID
	WorkerID     WorkerID
	Status       AssignmentStatus
	Output       string
	Error        string
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
	Status         AssignmentStatus `json:"status"`
	Result         string           `json:"result,omitempty"`
	Error          string           `json:"error,omitempty"`
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
