package events

import "time"

const ProtocolVersionV2 = "2026-07-13"

type AgentRole string

const (
	AgentRoleRoot   AgentRole = "root"
	AgentRoleWorker AgentRole = "worker"
)

type EventType string

const (
	EventMessageDelta            EventType = "message_delta"
	EventMessage                 EventType = "message"
	EventReasoningDelta          EventType = "reasoning_delta"
	EventToolStarted             EventType = "tool_started"
	EventToolOutput              EventType = "tool_output"
	EventToolFinished            EventType = "tool_finished"
	EventToolFailed              EventType = "tool_failed"
	EventPermissionRequest       EventType = "permission_required"
	EventMemoryInjected          EventType = "memory_injected"
	EventTodoUpdated             EventType = "todo_updated"
	EventSkillsInjected          EventType = "skills_injected"
	EventUsage                   EventType = "usage"
	EventFinish                  EventType = "finish"
	EventError                   EventType = "error"
	EventWorkerAssignmentUpdated EventType = "worker_assignment_updated"
)

type StreamKind string

const (
	StreamMessage           StreamKind = "message"
	StreamReasoning         StreamKind = "reasoning"
	StreamToolStdout        StreamKind = "tool_stdout"
	StreamToolStderr        StreamKind = "tool_stderr"
	StreamToolResultPreview StreamKind = "tool_result_preview"
)

type AgentRef struct {
	AgentID string    `json:"agent_id"`
	Role    AgentRole `json:"role"`
	Path    []string  `json:"path"`
	Name    string    `json:"name,omitempty"`
}

type StreamRef struct {
	StreamID string     `json:"stream_id"`
	Kind     StreamKind `json:"kind"`
	Seq      uint64     `json:"seq"`
	Final    bool       `json:"final"`
}

type Envelope struct {
	ProtocolVersion string         `json:"protocol_version"`
	EventID         string         `json:"event_id"`
	RootRunID       string         `json:"root_run_id"`
	RunID           string         `json:"run_id"`
	ParentRunID     string         `json:"parent_run_id,omitempty"`
	SessionID       string         `json:"session_id"`
	RootSeq         uint64         `json:"root_seq"`
	AgentSeq        uint64         `json:"agent_seq"`
	Agent           AgentRef       `json:"agent"`
	Stream          *StreamRef     `json:"stream,omitempty"`
	Type            EventType      `json:"type"`
	Payload         map[string]any `json:"payload"`
	CreatedAt       time.Time      `json:"created_at"`
}

// Envelope is the legacy v0.1 hierarchical event envelope (root/subagent model).
// It is retained ONLY for deserializing historical stored events. v0.2+ code MUST NOT produce Envelope.
// All new events and wire traffic use EnvelopeV2 exclusively.
type _deprecatedLegacyEnvelopeMarker struct{}

// EnvelopeV2 is the canonical v0.2 Worker event envelope. It has no root/parent hierarchy.
// An event belongs to exactly one Run and is attributed by Assignment + Worker.
type EventWorkerRef struct {
	ID         string `json:"id"`
	ProfileKey string `json:"profile_key,omitempty"`
}

type EnvelopeV2 struct {
	ProtocolVersion string         `json:"protocol_version"`
	EventID         string         `json:"event_id"`
	RunID           string         `json:"run_id"`
	SessionID       string         `json:"session_id"`
	AssignmentID    string         `json:"assignment_id"`
	Worker          EventWorkerRef `json:"worker"`
	RunSeq          uint64         `json:"run_seq"`
	WorkerSeq       uint64         `json:"worker_seq"`
	Stream          *StreamRef     `json:"stream,omitempty"`
	Type            EventType      `json:"type"`
	Payload         map[string]any `json:"payload"`
	CreatedAt       time.Time      `json:"created_at"`
}
