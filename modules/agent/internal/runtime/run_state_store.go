package runtime

import (
	"context"
	"sync"

	"redpanda/protocol/methods"
)

// RunState contains mutable state owned by Runtime for one run.
// WorkerPool assignments and MCP resources retain their own lifecycles.
type RunState struct {
	Cancel     context.CancelFunc
	RunSeq     uint64
	WorkerSeq  map[string]uint64
	Todos      []methods.TodoItemDTO
	registered bool
	paused     bool
	resume     chan struct{}
}

// RunStateStore is the single lifecycle owner for Runtime's per-run state.
// Its zero value is ready for use.
//
// Known limitation: state is in-memory and process-local by design. A Runtime
// crash loses in-flight tool-call intermediates and per-run cursors; Gateway's
// run_events table is the source of truth for event-sourced recovery, and
// RecoverStaleRuns marks orphaned runs as failed on Gateway startup. Do NOT
// add persistence here without solving the dual-write consistency problem
// (Runtime emit vs Gateway ack). See docs/plans/2026-07-19-convergence-wave.md
// Wave A.
type RunStateStore struct {
	mu   sync.RWMutex
	runs map[string]*RunState
}

func (s *RunStateStore) Register(runID string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateLocked(runID)
	if state.registered {
		return false
	}
	state.Cancel = cancel
	state.registered = true
	return true
}

func (s *RunStateStore) Cancel(runID string) context.CancelFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state := s.runs[runID]; state != nil {
		return state.Cancel
	}
	return nil
}

func (s *RunStateStore) Cancels() []context.CancelFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cancels := make([]context.CancelFunc, 0, len(s.runs))
	for _, state := range s.runs {
		if state.Cancel != nil {
			cancels = append(cancels, state.Cancel)
		}
	}
	return cancels
}

func (s *RunStateStore) Remove(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.runs[runID]; state != nil && state.paused {
		close(state.resume)
	}
	delete(s.runs, runID)
}

func (s *RunStateStore) Pause(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.runs[runID]
	if state == nil || !state.registered || state.paused {
		return false
	}
	state.paused = true
	state.resume = make(chan struct{})
	return true
}

func (s *RunStateStore) Resume(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.runs[runID]
	if state == nil || !state.paused {
		return false
	}
	state.paused = false
	close(state.resume)
	state.resume = nil
	return true
}

func (s *RunStateStore) WaitIfPaused(ctx context.Context, runID string) error {
	for {
		s.mu.RLock()
		state := s.runs[runID]
		if state == nil || !state.paused {
			s.mu.RUnlock()
			return nil
		}
		resume := state.resume
		s.mu.RUnlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resume:
		}
	}
}

func (s *RunStateStore) NextRunSeq(runID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateLocked(runID)
	state.RunSeq++
	return state.RunSeq
}

func (s *RunStateStore) NextWorkerSeq(runID string, workerID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateLocked(runID)
	if state.WorkerSeq == nil {
		state.WorkerSeq = make(map[string]uint64)
	}
	state.WorkerSeq[workerID]++
	return state.WorkerSeq[workerID]
}

func (s *RunStateStore) SetTodos(runID string, items []methods.TodoItemDTO) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrCreateLocked(runID)
	state.Todos = append([]methods.TodoItemDTO(nil), items...)
}

func (s *RunStateStore) Todos(runID string) []methods.TodoItemDTO {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.runs[runID]
	if state == nil || len(state.Todos) == 0 {
		return nil
	}
	return append([]methods.TodoItemDTO(nil), state.Todos...)
}

func (s *RunStateStore) ClearSnapshots(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.runs[runID]; state != nil {
		state.Todos = nil
	}
}

func (s *RunStateStore) getOrCreateLocked(runID string) *RunState {
	if s.runs == nil {
		s.runs = make(map[string]*RunState)
	}
	state := s.runs[runID]
	if state == nil {
		state = &RunState{}
		s.runs[runID] = state
	}
	return state
}
