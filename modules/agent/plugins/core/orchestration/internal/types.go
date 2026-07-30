package internal

import (
	"context"
	"encoding/json"
)

// HandlerFunc is the orchestration tool handler signature
// (mirrors registry.HandlerFunc).
type HandlerFunc func(ctx context.Context, toolCtx *ToolContext, args map[string]any) (*Result, error)

// ToolContext is copied from registry/registry.go for use by handlers.
type ToolContext struct {
	RunID        string
	SessionID    string
	AssignmentID string
	WorkerID     string
	WorkingDir   string
	Reply        any // will hold *methods.ReplyParams at runtime
}

// Result is copied from protocol/tools for use by handlers.
type Result struct {
	Output string `json:"output"`
	Error  string `json:"error"`
}

// marshalJSON returns compact JSON; errors are silently ignored
// (caller decides how to surface them).
func marshalJSON(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}
