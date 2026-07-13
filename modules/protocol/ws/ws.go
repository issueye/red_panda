package ws

import "encoding/json"

type Type string

const (
	TypeAuth     Type = "auth"
	TypeRequest  Type = "request"
	TypeResponse Type = "response"
	TypeEvent    Type = "event"
	TypeAck      Type = "ack"
	TypeError    Type = "error"
	TypePing     Type = "ping"
	TypePong     Type = "pong"
)

const (
	MethodRunStart          = "run.start"
	MethodRunSubscribe      = "run.subscribe"
	MethodRunResume         = "run.resume"
	MethodRunCancel         = "run.cancel"
	MethodPermissionResolve = "permission.resolve"
	MethodWorkerList        = "worker.list"
	MethodAssignmentCancel  = "worker.assignment.cancel"
	MethodAgentStatus       = "agent.status"
	EventRun                = "run.event"
	EventRuntimeStatus      = "runtime.status"
)

type Envelope struct {
	ID      string          `json:"id,omitempty"`
	Type    Type            `json:"type"`
	Method  string          `json:"method,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	Meta    map[string]any  `json:"meta,omitempty"`
}

type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Recoverable bool   `json:"recoverable,omitempty"`
}

type AuthPayload struct {
	Token  string     `json:"token"`
	Client ClientInfo `json:"client"`
}

type ClientInfo struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RunStartPayload struct {
	SessionID string         `json:"session_id"`
	Input     map[string]any `json:"input"`
	Options   map[string]any `json:"options,omitempty"`
	Subscribe bool           `json:"subscribe"`
}

type RunSubscribePayload struct {
	Runs []RunCursor `json:"runs"`
}

type RunResumePayload struct {
	LastSeen map[string]uint64 `json:"last_seen"`
}

type RunCursor struct {
	RunID    string `json:"run_id"`
	AfterSeq uint64 `json:"after_seq"`
}

type WorkerListPayload struct {
	RunID        string `json:"run_id,omitempty"`
	WorkerID     string `json:"worker_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
}

type AssignmentCancelPayload struct {
	RunID        string `json:"run_id"`
	AssignmentID string `json:"assignment_id"`
	Reason       string `json:"reason,omitempty"`
}
