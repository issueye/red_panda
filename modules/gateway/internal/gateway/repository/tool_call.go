package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

type ToolCallRepository struct {
	db *gorm.DB
}

func NewToolCallRepository(db *gorm.DB) ToolCallRepository {
	return ToolCallRepository{db: db}
}

func (r ToolCallRepository) Project(event events.EnvelopeV2) error {
	switch event.Type {
	case events.EventToolStarted:
		return r.projectStarted(event)
	case events.EventToolOutput:
		return r.projectOutput(event)
	case events.EventToolFinished, events.EventToolFailed:
		return r.projectFinished(event)
	default:
		return nil
	}
}

func (r ToolCallRepository) ListByRun(runID string, limit int) ([]model.ToolCall, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.ToolCall
	err := r.db.Where("run_id = ?", runID).Order("started_seq asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r ToolCallRepository) ListBySession(sessionID string, limit int) ([]model.ToolCall, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.ToolCall
	err := r.db.Where("session_id = ?", sessionID).Order("started_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r ToolCallRepository) projectStarted(event events.EnvelopeV2) error {
	toolCallID := stringPayload(event.Payload, "tool_call_id")
	if toolCallID == "" {
		return nil
	}
	argsRaw, _ := json.Marshal(anyPayload(event.Payload, "arguments"))
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := model.ToolCall{
		ID:            toolCallID,
		RunID:         event.RunID,
		SessionID:     event.SessionID,
		WorkerID:      event.Worker.ID,
		AssignmentID:  event.AssignmentID,
		ProfileKey:    event.Worker.ProfileKey,
		ToolName:      stringPayload(event.Payload, "tool_name"),
		DisplayName:   stringPayload(event.Payload, "display_name"),
		Risk:          stringPayload(event.Payload, "risk"),
		Policy:        stringPayload(event.Payload, "policy"),
		PolicyReason:  stringPayload(event.Payload, "policy_reason"),
		ArgumentsJSON: string(argsRaw),
		Status:        stringPayload(event.Payload, "status"),
		StartedSeq:    event.RunSeq,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"run_id":         row.RunID,
			"session_id":     row.SessionID,
			"worker_id":      row.WorkerID,
			"assignment_id":  row.AssignmentID,
			"profile_key":    row.ProfileKey,
			"tool_name":      row.ToolName,
			"display_name":   row.DisplayName,
			"risk":           row.Risk,
			"policy":         row.Policy,
			"policy_reason":  row.PolicyReason,
			"arguments_json": row.ArgumentsJSON,
			"status":         row.Status,
			"started_seq":    row.StartedSeq,
			"started_at":     row.StartedAt,
			"updated_at":     row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func (r ToolCallRepository) projectOutput(event events.EnvelopeV2) error {
	toolCallID := stringPayload(event.Payload, "tool_call_id")
	if toolCallID == "" {
		return nil
	}
	delta := stringPayload(event.Payload, "delta")
	if delta == "" {
		return nil
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var row model.ToolCall
	err := r.db.First(&row, "id = ?", toolCallID).Error
	if err == gorm.ErrRecordNotFound {
		row = model.ToolCall{
			ID:           toolCallID,
			RunID:        event.RunID,
			SessionID:    event.SessionID,
			WorkerID:     event.Worker.ID,
			AssignmentID: event.AssignmentID,
			ProfileKey:   event.Worker.ProfileKey,
			ToolName:     stringPayload(event.Payload, "tool_name"),
			Status:       "running",
			StartedSeq:   event.RunSeq,
			StartedAt:    now,
		}
	} else if err != nil {
		return err
	}
	row.Output += delta
	row.UpdatedAt = now
	if row.ID == "" {
		return fmt.Errorf("tool call id is empty")
	}
	return r.db.Save(&row).Error
}

func (r ToolCallRepository) projectFinished(event events.EnvelopeV2) error {
	toolCallID := stringPayload(event.Payload, "tool_call_id")
	if toolCallID == "" {
		return nil
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var row model.ToolCall
	err := r.db.First(&row, "id = ?", toolCallID).Error
	if err == gorm.ErrRecordNotFound {
		row = model.ToolCall{
			ID:           toolCallID,
			RunID:        event.RunID,
			SessionID:    event.SessionID,
			WorkerID:     event.Worker.ID,
			AssignmentID: event.AssignmentID,
			ProfileKey:   event.Worker.ProfileKey,
			StartedSeq:   event.RunSeq,
			StartedAt:    now,
		}
	} else if err != nil {
		return err
	}
	row.ToolName = firstNonEmpty(row.ToolName, stringPayload(event.Payload, "tool_name"))
	row.Status = firstNonEmpty(stringPayload(event.Payload, "status"), terminalStatus(event.Type))
	row.ExitCode = intPayload(event.Payload, "exit_code")
	row.Error = stringPayload(event.Payload, "error")
	if output := stringPayload(event.Payload, "output"); output != "" {
		row.Output = output
	}
	row.DurationMS = int64Payload(event.Payload, "duration_ms")
	row.FinishedSeq = event.RunSeq
	row.FinishedAt = &now
	row.UpdatedAt = now
	return r.db.Save(&row).Error
}

func terminalStatus(typ events.EventType) string {
	if typ == events.EventToolFailed {
		return "failed"
	}
	return "completed"
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func anyPayload(payload map[string]any, key string) any {
	if payload == nil {
		return nil
	}
	return payload[key]
}

func intPayload(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func int64Payload(payload map[string]any, key string) int64 {
	switch value := payload[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
