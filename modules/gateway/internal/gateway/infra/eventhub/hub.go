package eventhub

import (
	"sync"

	"redpanda/protocol/events"
	protows "redpanda/protocol/ws"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan events.EnvelopeV2]struct{}
	// broadcast fans out non-run WS envelopes (session.upserted, etc.) to every connected client.
	broadcast map[chan protows.Envelope]struct{}
}

func New() *Hub {
	return &Hub{
		subscribers: map[string]map[chan events.EnvelopeV2]struct{}{},
		broadcast:   map[chan protows.Envelope]struct{}{},
	}
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

// SubscribeBroadcast receives global (non-run) WS events. Callers must drain until cancel.
func (h *Hub) SubscribeBroadcast() (<-chan protows.Envelope, func()) {
	ch := make(chan protows.Envelope, 32)
	h.mu.Lock()
	h.broadcast[ch] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.broadcast[ch]; ok {
			delete(h.broadcast, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// Broadcast sends a WS envelope to every connected desktop (best-effort, drop if full).
func (h *Hub) Broadcast(msg protows.Envelope) {
	if h == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.broadcast {
		select {
		case ch <- msg:
		default:
		}
	}
}
