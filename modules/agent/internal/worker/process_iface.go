package worker

import (
	"context"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

// Process is the reusable Runtime transport owned by a Worker slot.
type Process interface {
	Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.EnvelopeV2)) error
	Cancel(ctx context.Context, runID string, reason string) error
	Close(ctx context.Context) error
}
