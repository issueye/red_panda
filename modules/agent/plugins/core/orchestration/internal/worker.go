package internal

import (
	"context"
	"fmt"
)

// workerHandlerDelegate — worker.delegate (SelfManagedToolTimeout, opsOnly=false).
// Requires the Runtime (executeWorkerDelegate / executeDelegatedAssignment).
func workerHandlerDelegate(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// workerHandlerList — worker.list (LocalToolTimeout, opsOnly=false).
// Requires r.workerPool.Snapshot() from the Runtime.
func workerHandlerList(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// workerHandlerCancel — worker.cancel (LocalToolTimeout, opsOnly=false).
// Requires r.workerPool.Cancel() and r.workerPool.Snapshot() from the Runtime.
func workerHandlerCancel(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// workerHandlerPoolStatus — worker.pool_status (LocalToolTimeout, opsOnly=true).
// Requires r.workerPool.Snapshot() from the Runtime.
func workerHandlerPoolStatus(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// workerHandlerSend — worker.send (LocalToolTimeout, opsOnly=true).
// Requires r.workerPool.Send() and r.workerPool.Snapshot() from the Runtime.
func workerHandlerSend(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// workerHandlerReceive — worker.receive (LocalToolTimeout, opsOnly=true).
// Requires r.workerPool.Receive() and r.workerPool.Snapshot() from the Runtime.
func workerHandlerReceive(_ context.Context, _ *ToolContext, _ map[string]any) (*Result, error) {
	return nil, fmt.Errorf("not yet migrated")
}

// Handler exports.
var (
	HandlerWorkerDelegate HandlerFunc = workerHandlerDelegate
	HandlerWorkerList     HandlerFunc = workerHandlerList
	HandlerWorkerCancel   HandlerFunc = workerHandlerCancel
	HandlerWorkerPoolStatus HandlerFunc = workerHandlerPoolStatus
	HandlerWorkerSend     HandlerFunc = workerHandlerSend
	HandlerWorkerReceive  HandlerFunc = workerHandlerReceive
)
