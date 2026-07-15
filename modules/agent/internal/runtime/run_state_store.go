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
	Goal       *runGoalState
	registered bool
}

// RunStateStore is the single lifecycle owner for Runtime's per-run state.
// Its zero value is ready for use.
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
	delete(s.runs, runID)
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

func (s *RunStateStore) SetGoal(runID string, goal *runGoalState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if goal == nil {
		if state := s.runs[runID]; state != nil {
			state.Goal = nil
		}
		return
	}
	state := s.getOrCreateLocked(runID)
	state.Goal = cloneRunGoalState(goal)
}

func (s *RunStateStore) Goal(runID string) *runGoalState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state := s.runs[runID]; state != nil {
		return cloneRunGoalState(state.Goal)
	}
	return nil
}

func (s *RunStateStore) ClearSnapshots(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.runs[runID]; state != nil {
		state.Todos = nil
		state.Goal = nil
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

func cloneRunGoalState(state *runGoalState) *runGoalState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.Goal.Criteria = append([]methods.GoalCriterionDTO(nil), state.Goal.Criteria...)
	clone.Goal.Constraints = append([]string(nil), state.Goal.Constraints...)
	clone.Goal.Actions = append([]methods.GoalActionDTO(nil), state.Goal.Actions...)
	if state.Goal.LastAssessment != nil {
		assessment := *state.Goal.LastAssessment
		assessment.Criteria = append([]methods.GoalCriterionDTO(nil), state.Goal.LastAssessment.Criteria...)
		clone.Goal.LastAssessment = &assessment
	}
	return &clone
}
