package tools

import "redpanda/protocol/methods"

// ToolRunContext carries trusted Runtime identity into host-backed tools.
// Tool registration and execution are owned by runtime/registry.
type ToolRunContext struct {
	WorkingDir   string
	RunID        string
	SessionID    string
	AssignmentID string
	WorkerID     string
	Reply        *methods.ReplyParams
}
