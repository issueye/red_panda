package worker

import (
	"context"
	"sync"
	"time"
)

// Mailbox is a bounded, non-blocking delivery queue owned by one Worker.
type Mailbox struct {
	mu       sync.RWMutex
	messages chan WorkerMessage
	closed   bool
}

func NewMailbox(capacity int) (*Mailbox, error) {
	if capacity <= 0 {
		return nil, ErrInvalidConfig
	}
	return &Mailbox{messages: make(chan WorkerMessage, capacity)}, nil
}

func (m *Mailbox) Deliver(message WorkerMessage) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return ErrMailboxClosed
	}
	select {
	case m.messages <- message:
		return nil
	default:
		return ErrMailboxFull
	}
}

func (m *Mailbox) Receive(ctx context.Context, runID string, assignmentID AssignmentID, done <-chan struct{}) (WorkerMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return WorkerMessage{}, ctx.Err()
		case <-done:
			return WorkerMessage{}, ErrAssignmentNotActive
		case message, ok := <-m.messages:
			if !ok {
				return WorkerMessage{}, ErrMailboxClosed
			}
			if message.RunID != runID || message.ToAssignmentID != assignmentID || (!message.ExpiresAt.IsZero() && !message.ExpiresAt.After(time.Now().UTC())) {
				continue
			}
			return message, nil
		}
	}
}

func (m *Mailbox) Len() int { return len(m.messages) }
func (m *Mailbox) Cap() int { return cap(m.messages) }

func (m *Mailbox) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.messages)
}
