package service

import (
	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

// Run HTTP/WS DTO mappers.
func runRecordDTO(row model.RunRecord) RunRecordDTO {
	return RunRecordDTO{
		ID:            row.ID,
		SessionID:     row.SessionID,
		WorkspaceRoot: row.WorkspaceRoot,
		RuntimeMode:   row.RuntimeMode,
		Status:        row.Status,
		Input:         row.Input,
		LastEventType: row.LastEventType,
		LastRunSeq:    row.LastRootSeq,
		MessageCount:  row.MessageCount,
		ToolCount:     row.ToolCount,
		Error:         row.Error,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func runEventDTO(event events.EnvelopeV2) RunEventDTO {
	streamKind := ""
	if event.Stream != nil {
		streamKind = string(event.Stream.Kind)
	}
	return RunEventDTO{
		ID:           event.EventID,
		Type:         string(event.Type),
		RunID:        event.RunID,
		SessionID:    event.SessionID,
		AssignmentID: event.AssignmentID,
		RunSeq:       event.RunSeq,
		WorkerSeq:    event.WorkerSeq,
		WorkerID:     event.Worker.ID,
		ProfileKey:   event.Worker.ProfileKey,
		StreamKind:   streamKind,
		Payload:      event.Payload,
		CreatedAt:    event.CreatedAt,
	}
}
