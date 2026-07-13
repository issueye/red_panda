package repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

type RunRecordRepository struct {
	db *gorm.DB
}

func NewRunRecordRepository(db *gorm.DB) RunRecordRepository {
	return RunRecordRepository{db: db}
}

func (r RunRecordRepository) Start(run model.RunRecord) error {
	now := run.StartedAt
	if now.IsZero() {
		now = time.Now().UTC()
		run.StartedAt = now
	}
	run.UpdatedAt = now
	if run.Status == "" {
		run.Status = "running"
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"session_id":     run.SessionID,
			"workspace_root": run.WorkspaceRoot,
			"runtime_mode":   run.RuntimeMode,
			"status":         run.Status,
			"input":          run.Input,
			"started_at":     run.StartedAt,
			"updated_at":     run.UpdatedAt,
		}),
	}).Create(&run).Error
}

func (r RunRecordRepository) ProjectEvent(event events.EnvelopeV2) error {
	if event.RunID == "" {
		return nil
	}
	now := event.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var row model.RunRecord
	err := r.db.First(&row, "id = ?", event.RunID).Error
	if err == gorm.ErrRecordNotFound {
		row = model.RunRecord{
			ID:        event.RunID,
			SessionID: event.SessionID,
			Status:    "running",
			StartedAt: now,
			UpdatedAt: now,
		}
		applyRunEventProjection(&row, event, now)
		return r.db.Create(&row).Error
	} else if err != nil {
		return err
	}
	applyRunEventProjection(&row, event, now)
	updates := map[string]any{
		"session_id":      row.SessionID,
		"last_event_type": row.LastEventType,
		"last_root_seq":   row.LastRootSeq,
		"error":           row.Error,
		"status":          row.Status,
		"finished_at":     row.FinishedAt,
		"updated_at":      row.UpdatedAt,
	}
	if event.Type == events.EventMessageDelta {
		updates["message_count"] = gorm.Expr("message_count + ?", 1)
	}
	if event.Type == events.EventToolStarted {
		updates["tool_count"] = gorm.Expr("tool_count + ?", 1)
	}
	return r.db.Model(&model.RunRecord{}).Where("id = ?", event.RunID).Updates(updates).Error
}

func applyRunEventProjection(row *model.RunRecord, event events.EnvelopeV2, now time.Time) {
	row.SessionID = firstNonEmpty(row.SessionID, event.SessionID)
	row.LastEventType = string(event.Type)
	row.LastRootSeq = event.RunSeq
	row.UpdatedAt = now
	if event.Type == events.EventMessageDelta {
		row.MessageCount++
	}
	if event.Type == events.EventToolStarted {
		row.ToolCount++
	}
	if event.Type == events.EventError {
		row.Status = firstNonEmpty(stringPayload(event.Payload, "status"), "failed")
		row.Error = firstNonEmpty(stringPayload(event.Payload, "message"), row.Error)
	}
	if event.Type == events.EventFinish {
		row.Status = firstNonEmpty(stringPayload(event.Payload, "status"), "completed")
		row.FinishedAt = &now
	}
}

func (r RunRecordRepository) Get(runID string) (model.RunRecord, error) {
	var row model.RunRecord
	err := r.db.First(&row, "id = ?", runID).Error
	return row, err
}

func (r RunRecordRepository) ListBySession(sessionID string, limit int) ([]model.RunRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.RunRecord
	err := r.db.Where("session_id = ?", sessionID).Order("started_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// CountActive returns how many runs are currently running or waiting for permission.
func (r RunRecordRepository) CountActive() (int64, error) {
	var count int64
	err := r.db.Model(&model.RunRecord{}).
		Where("status IN ?", []string{"running", "waiting_permission"}).
		Count(&count).Error
	return count, err
}

// CountActiveBySession returns active runs for one session.
func (r RunRecordRepository) CountActiveBySession(sessionID string) (int64, error) {
	var count int64
	err := r.db.Model(&model.RunRecord{}).
		Where("session_id = ? AND status IN ?", sessionID, []string{"running", "waiting_permission"}).
		Count(&count).Error
	return count, err
}

// ListActive returns active runs ordered by start time.
func (r RunRecordRepository) ListActive(limit int) ([]model.RunRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.RunRecord
	err := r.db.Where("status IN ?", []string{"running", "waiting_permission"}).
		Order("started_at asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ListActiveBySession returns active runs for one session ordered by start time.
func (r RunRecordRepository) ListActiveBySession(sessionID string, limit int) ([]model.RunRecord, error) {
	if sessionID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []model.RunRecord
	err := r.db.Where("session_id = ? AND status IN ?", sessionID, []string{"running", "waiting_permission"}).
		Order("started_at asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// Finish marks a run as terminal so it no longer consumes the concurrent budget.
func (r RunRecordRepository) Finish(runID string, status string, errText string) error {
	if runID == "" {
		return nil
	}
	if status == "" {
		status = "completed"
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":      status,
		"finished_at": now,
		"updated_at":  now,
	}
	if errText != "" {
		updates["error"] = errText
	}
	return r.db.Model(&model.RunRecord{}).Where("id = ?", runID).Updates(updates).Error
}

func (r RunRecordRepository) RefreshToolCount(runID string) error {
	if runID == "" {
		return nil
	}
	var count int64
	if err := r.db.Model(&model.ToolCall{}).Where("run_id = ?", runID).Count(&count).Error; err != nil {
		return err
	}
	return r.db.Model(&model.RunRecord{}).
		Where("id = ?", runID).
		Update("tool_count", count).Error
}
