package subagent

import (
	"context"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

// Process 表示子代理 Runtime 进程（stdio/IPC JSON-RPC）。
type Process interface {
	Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error
	Cancel(ctx context.Context, runID string, reason string) error
	Close(ctx context.Context) error
}
