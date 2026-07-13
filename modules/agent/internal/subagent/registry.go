package subagent

import (
	"context"
	"sync"
	"time"

	"redpanda/protocol/methods"
)

type Registration struct {
	SubAgentID      string
	Name            string
	Backend         string
	RootRunID       string
	ParentRunID     string
	ParentSessionID string
	ChildRunID      string
	Summary         string
	Cancel          context.CancelFunc
}

type registryState struct {
	record methods.SubAgentRecord
	cancel context.CancelFunc
}

type Registry struct {
	mu     sync.Mutex
	states map[string]*registryState
}

func NewRegistry() *Registry {
	return &Registry{states: map[string]*registryState{}}
}

func (r *Registry) Register(registration Registration) methods.SubAgentRecord {
	if r == nil {
		return methods.SubAgentRecord{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	parentRunID := registration.ParentRunID
	if parentRunID == "" {
		parentRunID = registration.RootRunID
	}
	childRunID := registration.ChildRunID
	if childRunID == "" && registration.RootRunID != "" && registration.SubAgentID != "" {
		childRunID = registration.RootRunID + ":subagent:" + registration.SubAgentID
	}
	summary := registration.Summary
	if summary == "" {
		summary = registration.Name + " subagent started"
	}
	record := methods.SubAgentRecord{
		SubAgentID:      registration.SubAgentID,
		Name:            registration.Name,
		Backend:         registration.Backend,
		Status:          "running",
		RootRunID:       registration.RootRunID,
		ParentRunID:     parentRunID,
		ParentSessionID: registration.ParentSessionID,
		ChildRunID:      childRunID,
		Summary:         summary,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.mu.Lock()
	r.states[registration.SubAgentID] = &registryState{record: record, cancel: registration.Cancel}
	r.mu.Unlock()
	return record
}

func (r *Registry) Finish(rootRunID string, subAgentID string, status string, summary string, errText string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID || terminalSubAgentStatus(state.record.Status) {
		return false
	}
	updateRegistryState(state, status, summary, errText)
	return true
}

func (r *Registry) ForceFinish(rootRunID string, subAgentID string, status string, summary string, errText string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return false
	}
	updateRegistryState(state, status, summary, errText)
	return true
}

func (r *Registry) Lookup(rootRunID string, subAgentID string) (methods.SubAgentRecord, bool) {
	if r == nil {
		return methods.SubAgentRecord{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return methods.SubAgentRecord{}, false
	}
	return state.record, true
}

func (r *Registry) List(params methods.SubAgentsParams) []methods.SubAgentRecord {
	if r == nil {
		return []methods.SubAgentRecord{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]methods.SubAgentRecord, 0, len(r.states))
	for _, state := range r.states {
		if params.RunID != "" && state.record.RootRunID != params.RunID {
			continue
		}
		if params.SubAgentID != "" && state.record.SubAgentID != params.SubAgentID {
			continue
		}
		items = append(items, state.record)
	}
	return items
}

func (r *Registry) Cancel(rootRunID string, subAgentID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	state := r.states[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID || state.cancel == nil {
		r.mu.Unlock()
		return false
	}
	cancel := state.cancel
	state.cancel = nil
	r.mu.Unlock()
	cancel()
	return true
}

func (r *Registry) Remove(rootRunID string, subAgentID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return false
	}
	delete(r.states, subAgentID)
	return true
}

func updateRegistryState(state *registryState, status string, summary string, errText string) {
	state.record.Status = status
	state.record.Summary = summary
	state.record.Error = errText
	state.record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	state.cancel = nil
}

func terminalSubAgentStatus(status string) bool {
	switch status {
	case "cancelled", "failed", "completed", "reset":
		return true
	default:
		return false
	}
}
