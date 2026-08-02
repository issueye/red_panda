package service

import (
	"time"

	"redpanda/protocol/events"
)

// RunMessageStreamDTO is the authoritative UI projection of contiguous model
// output events. It is intentionally separate from model-facing messages.
type RunMessageStreamDTO struct {
	ID           string    `json:"id"`
	Role         string    `json:"role"`
	Text         string    `json:"text"`
	RunID        string    `json:"run_id"`
	AssignmentID string    `json:"assignment_id,omitempty"`
	WorkerID     string    `json:"worker_id,omitempty"`
	ProfileKey   string    `json:"profile_key,omitempty"`
	Visibility   string    `json:"visibility,omitempty"`
	RunSeq       uint64    `json:"run_seq"`
	EndRunSeq    uint64    `json:"end_run_seq"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r RunService) MessageStreamsBySession(sessionID string) ([]RunMessageStreamDTO, error) {
	items, err := r.repos.RunEvents.ListMessageStreamsBySession(sessionID)
	if err != nil {
		return nil, err
	}
	streams := make([]RunMessageStreamDTO, 0)
	lastByRun := make(map[string]int)
	for _, event := range items {
		delta, _ := event.Payload["delta"].(string)
		if delta == "" {
			continue
		}
		role := "assistant"
		if event.Type == events.EventReasoningDelta {
			role = "reasoning"
		}
		visibility := payloadString(event.Payload, "visibility")
		if index, ok := lastByRun[event.RunID]; ok {
			previous := &streams[index]
			if previous.Role == role &&
				previous.AssignmentID == event.AssignmentID &&
				previous.WorkerID == event.Worker.ID &&
				previous.ProfileKey == event.Worker.ProfileKey &&
				previous.Visibility == visibility &&
				event.RunSeq == previous.EndRunSeq+1 {
				previous.Text += delta
				previous.EndRunSeq = event.RunSeq
				continue
			}
		}
		streams = append(streams, RunMessageStreamDTO{
			ID:           event.EventID,
			Role:         role,
			Text:         delta,
			RunID:        event.RunID,
			AssignmentID: event.AssignmentID,
			WorkerID:     event.Worker.ID,
			ProfileKey:   event.Worker.ProfileKey,
			Visibility:   visibility,
			RunSeq:       event.RunSeq,
			EndRunSeq:    event.RunSeq,
			CreatedAt:    event.CreatedAt,
		})
		lastByRun[event.RunID] = len(streams) - 1
	}
	return streams, nil
}
