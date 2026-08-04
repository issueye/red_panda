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
	WorkerSeq    uint64    `json:"worker_seq"`
	EndWorkerSeq uint64    `json:"end_worker_seq"`
	StreamID     string    `json:"stream_id,omitempty"`
	StreamKind   string    `json:"stream_kind,omitempty"`
	StreamSeq    uint64    `json:"stream_seq,omitempty"`
	EndStreamSeq uint64    `json:"end_stream_seq,omitempty"`
	StreamFinal  bool      `json:"stream_final"`
	CreatedAt    time.Time `json:"created_at"`
}

type runMessageStreamKey struct {
	RunID        string
	AssignmentID string
	WorkerID     string
	Role         string
	Visibility   string
	StreamID     string
	StreamKind   string
}

func (r RunService) MessageStreamsBySession(sessionID string) ([]RunMessageStreamDTO, error) {
	items, err := r.repos.RunEvents.ListMessageStreamsBySession(sessionID)
	if err != nil {
		return nil, err
	}
	streams := make([]RunMessageStreamDTO, 0)
	lastByStream := make(map[runMessageStreamKey]int)
	for _, event := range items {
		delta, _ := event.Payload["delta"].(string)
		role := "assistant"
		streamKind := string(events.StreamMessage)
		if event.Type == events.EventReasoningDelta {
			role = "reasoning"
			streamKind = string(events.StreamReasoning)
		}
		visibility := payloadString(event.Payload, "visibility")
		streamID := ""
		streamSeq := uint64(0)
		streamFinal := false
		if event.Stream != nil {
			streamID = event.Stream.StreamID
			streamKind = string(event.Stream.Kind)
			streamSeq = event.Stream.Seq
			streamFinal = event.Stream.Final
		}
		key := runMessageStreamKey{
			RunID: event.RunID, AssignmentID: event.AssignmentID, WorkerID: event.Worker.ID,
			Role: role, Visibility: visibility, StreamID: streamID, StreamKind: streamKind,
		}
		if delta == "" {
			if index, ok := lastByStream[key]; ok && streamFinal {
				previous := &streams[index]
				previous.EndRunSeq = event.RunSeq
				previous.EndWorkerSeq = event.WorkerSeq
				previous.EndStreamSeq = streamSeq
				previous.StreamFinal = true
			}
			continue
		}
		if index, ok := lastByStream[key]; ok {
			previous := &streams[index]
			workerSequenceContinues := event.WorkerSeq > 0 && previous.EndWorkerSeq > 0 &&
				event.WorkerSeq == previous.EndWorkerSeq+1
			legacySequenceContinues := event.WorkerSeq == 0 && previous.EndWorkerSeq == 0 &&
				event.RunSeq == previous.EndRunSeq+1
			if !previous.StreamFinal && (workerSequenceContinues || legacySequenceContinues) {
				previous.Text += delta
				previous.EndRunSeq = event.RunSeq
				previous.EndWorkerSeq = event.WorkerSeq
				previous.EndStreamSeq = streamSeq
				previous.StreamFinal = streamFinal
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
			WorkerSeq:    event.WorkerSeq,
			EndWorkerSeq: event.WorkerSeq,
			StreamID:     streamID,
			StreamKind:   streamKind,
			StreamSeq:    streamSeq,
			EndStreamSeq: streamSeq,
			StreamFinal:  streamFinal,
			CreatedAt:    event.CreatedAt,
		})
		lastByStream[key] = len(streams) - 1
	}
	return streams, nil
}
