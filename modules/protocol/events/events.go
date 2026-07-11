package events

import "time"

const ProtocolVersion = "2026-07-09"

type AgentRole string

const (
	AgentRoleRoot     AgentRole = "root"
	AgentRoleSubAgent AgentRole = "subagent"
)

type EventType string

const (
	EventMessageDelta      EventType = "message_delta"
	EventMessage           EventType = "message"
	EventReasoningDelta    EventType = "reasoning_delta"
	EventToolStarted       EventType = "tool_started"
	EventToolOutput        EventType = "tool_output"
	EventToolFinished      EventType = "tool_finished"
	EventToolFailed        EventType = "tool_failed"
	EventPermissionRequest EventType = "permission_required"
	EventSubAgentUpdate    EventType = "subagent_update"
	EventMemoryInjected    EventType = "memory_injected"
	EventSkillsInjected    EventType = "skills_injected"
	EventUsage             EventType = "usage"
	EventFinish            EventType = "finish"
	EventError             EventType = "error"
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
	AgentID       string    `json:"agent_id"`
	Role          AgentRole `json:"role"`
	SubAgentID    string    `json:"subagent_id,omitempty"`
	ParentAgentID string    `json:"parent_agent_id,omitempty"`
	Path          []string  `json:"path"`
	Name          string    `json:"name,omitempty"`
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
