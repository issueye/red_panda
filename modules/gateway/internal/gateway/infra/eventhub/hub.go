package eventhub

import (
	"sync"

	"redpanda/protocol/events"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan events.Envelope]struct{}
}

func New() *Hub {
	return &Hub{subscribers: map[string]map[chan events.Envelope]struct{}{}}
}

func (h *Hub) Subscribe(rootRunID string) (<-chan events.Envelope, func()) {
	ch := make(chan events.Envelope, 64)

	h.mu.Lock()
	if h.subscribers[rootRunID] == nil {
		h.subscribers[rootRunID] = map[chan events.Envelope]struct{}{}
	}
	h.subscribers[rootRunID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if subs := h.subscribers[rootRunID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(h.subscribers, rootRunID)
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (h *Hub) Publish(event events.Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers[event.RootRunID] {
		select {
		case ch <- event:
		default:
		}
	}
}
