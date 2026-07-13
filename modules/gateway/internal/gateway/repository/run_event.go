package repository

import (
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

type RunEventRepository struct {
	db *gorm.DB
}

func NewRunEventRepository(db *gorm.DB) RunEventRepository {
	return RunEventRepository{db: db}
}

func (r RunEventRepository) Save(event events.Envelope) error {
	_, err := r.SaveOnce(event)
	return err
}

// SaveOnce persists one event ID and reports whether this call inserted it.
// Duplicate transport delivery must not repeat projections or broadcasts.
func (r RunEventRepository) SaveOnce(event events.Envelope) (bool, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return false, err
	}
	result := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.RunEvent{
		ID:          event.EventID,
		RootRunID:   event.RootRunID,
		RootSeq:     event.RootSeq,
		Type:        string(event.Type),
		PayloadJSON: string(raw),
		CreatedAt:   event.CreatedAt,
	})
	return result.RowsAffected == 1, result.Error
}

func (r RunEventRepository) ListAfter(rootRunID string, afterSeq uint64, limit int) ([]events.Envelope, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var rows []model.RunEvent
	if err := r.db.
		Where("root_run_id = ? AND root_seq > ?", rootRunID, afterSeq).
		Order("root_seq asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]events.Envelope, 0, len(rows))
	for _, row := range rows {
		var event events.Envelope
		if err := json.Unmarshal([]byte(row.PayloadJSON), &event); err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, nil
}
