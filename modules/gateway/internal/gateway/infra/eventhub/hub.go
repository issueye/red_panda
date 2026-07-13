package eventhub

import (
	"sync"

	"redpanda/protocol/events"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan events.EnvelopeV2]struct{}
}

func New() *Hub {
	return &Hub{subscribers: map[string]map[chan events.EnvelopeV2]struct{}{}}
}

func (h *Hub) Subscribe(runID string) (<-chan events.EnvelopeV2, func()) {
	ch := make(chan events.EnvelopeV2, 64)

	h.mu.Lock()
	if h.subscribers[runID] == nil {
		h.subscribers[runID] = map[chan events.EnvelopeV2]struct{}{}
	}
	h.subscribers[runID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if subs := h.subscribers[runID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(h.subscribers, runID)
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (h *Hub) Publish(event events.EnvelopeV2) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[event.RunID] {
		select {
		case ch <- event:
		default:
		}
	}
}
