package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type SessionLineageRepository struct {
	db *gorm.DB
}

func NewSessionLineageRepository(db *gorm.DB) SessionLineageRepository {
	return SessionLineageRepository{db: db}
}

func (r SessionLineageRepository) Create(row model.SessionLineage) (model.SessionLineage, error) {
	now := time.Now().UTC()
	if row.ID == "" {
		row.ID = fmt.Sprintf("lineage_%d", now.UnixNano())
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	return row, r.db.Create(&row).Error
}

func (r SessionLineageRepository) ListForSession(sessionID string) ([]model.SessionLineage, error) {
	var rows []model.SessionLineage
	err := r.db.
		Where("source_session_id = ? OR target_session_id = ?", sessionID, sessionID).
		Order("created_at asc").
		Find(&rows).Error
	return rows, err
}
