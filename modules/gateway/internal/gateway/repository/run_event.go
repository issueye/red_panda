package repository

import (
	"encoding/json"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

type RunEventRepository struct {
	db *gorm.DB
}

func NewRunEventRepository(db *gorm.DB) RunEventRepository {
	return RunEventRepository{db: db}
}

func (r RunEventRepository) Save(event events.EnvelopeV2) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.db.Create(&model.RunEvent{
		ID:          event.EventID,
		RunID:       event.RunID,
		RunSeq:      event.RunSeq,
		Type:        string(event.Type),
		PayloadJSON: string(raw),
		CreatedAt:   event.CreatedAt,
	}).Error
}

func (r RunEventRepository) ListAfter(runID string, afterSeq uint64, limit int) ([]events.EnvelopeV2, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var rows []model.RunEvent
	if err := r.db.
		Where("run_id = ? AND run_seq > ?", runID, afterSeq).
		Order("run_seq asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]events.EnvelopeV2, 0, len(rows))
	for _, row := range rows {
		var event events.EnvelopeV2
		if err := json.Unmarshal([]byte(row.PayloadJSON), &event); err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, nil
}

// ListMessageStreamsBySession returns the complete persisted text/reasoning
// event source for Desktop transcript reconstruction.
func (r RunEventRepository) ListMessageStreamsBySession(sessionID string) ([]events.EnvelopeV2, error) {
	var rows []model.RunEvent
	if err := r.db.Model(&model.RunEvent{}).
		Select("run_events.*").
		Joins("JOIN run_records ON run_records.id = run_events.run_id").
		Where("run_records.session_id = ? AND run_events.type IN ?", sessionID, []string{
			string(events.EventMessageDelta),
			string(events.EventReasoningDelta),
		}).
		Order("run_events.created_at asc, run_events.run_id asc, run_events.run_seq asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]events.EnvelopeV2, 0, len(rows))
	for _, row := range rows {
		var event events.EnvelopeV2
		if err := json.Unmarshal([]byte(row.PayloadJSON), &event); err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, nil
}
