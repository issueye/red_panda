package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type SessionCompactionRepository struct {
	db *gorm.DB
}

func NewSessionCompactionRepository(db *gorm.DB) SessionCompactionRepository {
	return SessionCompactionRepository{db: db}
}

func (r SessionCompactionRepository) Create(row model.SessionCompaction) (model.SessionCompaction, error) {
	now := time.Now().UTC()
	if row.ID == "" {
		row.ID = fmt.Sprintf("compact_%d", now.UnixNano())
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	return row, r.db.Create(&row).Error
}

func (r SessionCompactionRepository) ListForSession(sessionID string) ([]model.SessionCompaction, error) {
	var rows []model.SessionCompaction
	err := r.db.
		Where("source_session_id = ? OR target_session_id = ?", sessionID, sessionID).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}

func (r SessionCompactionRepository) LatestAppliedInPlace(sessionID string) (model.SessionCompaction, error) {
	var row model.SessionCompaction
	err := r.db.
		Where("source_session_id = ? AND target_session_id = ? AND status = ?", sessionID, sessionID, "applied").
		Order("created_at desc").
		First(&row).Error
	return row, err
}

func (r SessionCompactionRepository) SupersedeAppliedInPlace(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return r.db.Model(&model.SessionCompaction{}).
		Where("source_session_id = ? AND target_session_id = ? AND status = ?", sessionID, sessionID, "applied").
		Updates(map[string]any{
			"status":     "superseded",
			"updated_at": time.Now().UTC(),
		}).Error
}
