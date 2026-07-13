package subagent

import (
	"context"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

// Process is a child agent Runtime process (stdio/IPC JSON-RPC).
type Process interface {
	Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error
	Cancel(ctx context.Context, runID string, reason string) error
	Close(ctx context.Context) error
}
