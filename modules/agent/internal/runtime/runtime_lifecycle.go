package runtime

import (
	"context"
	"strings"
)

// Per-run registration, cancellation, and Runtime.Close.
func (r *Runtime) registerRun(runID string, cancel context.CancelFunc) bool {
	return r.runStates.Register(runID, cancel)
}

func (r *Runtime) unregisterRun(runID string) {
	r.runStates.Remove(runID)
	r.clearRunRegistry(runID)
}

func (r *Runtime) cancelRun(runID string) bool {
	return r.cancelRunCount(runID, "run cancelled") > 0
}

func (r *Runtime) cancelRunCount(runID string, reason string) int {
	cancel := r.runStates.Cancel(runID)

	cancelledAssignments := 0
	if r.workerPool != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "run cancelled"
		}
		cancelledAssignments = r.workerPool.CancelRun(context.Background(), runID, reason)
	}
	if cancel != nil {
		cancel()
		if cancelledAssignments == 0 {
			cancelledAssignments = 1
		}
	}
	return cancelledAssignments
}

// Close stops all active runs and releases Runtime-owned execution resources.
func (r *Runtime) Close(ctx context.Context) error {
	cancels := r.runStates.Cancels()
	for _, cancel := range cancels {
		cancel()
	}
	if r.mcp != nil {
		r.mcp.CloseAll()
	}
	if r.workerPool != nil {
		return r.workerPool.Close(ctx)
	}
	return nil
}
