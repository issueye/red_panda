package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type AttachmentRepository struct {
	db *gorm.DB
}

func NewAttachmentRepository(db *gorm.DB) AttachmentRepository {
	return AttachmentRepository{db: db}
}

func (r AttachmentRepository) Create(att model.Attachment) (model.Attachment, error) {
	now := time.Now().UTC()
	if att.CreatedAt.IsZero() {
		att.CreatedAt = now
	}
	if att.LastRefAt.IsZero() {
		att.LastRefAt = now
	}
	if err := r.db.Create(&att).Error; err != nil {
		return model.Attachment{}, err
	}
	return att, nil
}

// Get returns a non-deleted attachment by id, scoped to sessionID when non-empty.
func (r AttachmentRepository) Get(id string, sessionID string) (model.Attachment, error) {
	var row model.Attachment
	query := r.db.Where("id = ? AND deleted_at IS NULL", id)
	if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}
	err := query.First(&row).Error
	return row, err
}

// TouchLastRef records a reference time for GC heuristics.
func (r AttachmentRepository) TouchLastRef(id string, at time.Time) error {
	return r.db.Model(&model.Attachment{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("last_ref_at", at.UTC()).Error
}

func (r AttachmentRepository) ListBySession(sessionID string) ([]model.Attachment, error) {
	var rows []model.Attachment
	err := r.db.Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}

// SumBytesBySession sums stored byte sizes for a session's live attachments,
// used to enforce the per-session quota (docs/51 §9).
func (r AttachmentRepository) SumBytesBySession(sessionID string) (int64, error) {
	var sum struct{ Total int64 }
	err := r.db.Model(&model.Attachment{}).
		Select("COALESCE(SUM(byte_size), 0) AS total").
		Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Scan(&sum).Error
	return sum.Total, err
}

// FindBySHA256 returns a live upload attachment with the same content hash for
// optional dedupe (caller scopes kind/origin as needed).
func (r AttachmentRepository) FindBySHA256(sha256 string, sessionID string) (model.Attachment, error) {
	var row model.Attachment
	err := r.db.Where("sha256 = ? AND deleted_at IS NULL", sha256).
		Order("created_at asc").First(&row).Error
	if sessionID != "" {
		// Prefer an in-session match when caller asks for one.
		var preferred model.Attachment
		if err2 := r.db.Where("sha256 = ? AND session_id = ? AND deleted_at IS NULL", sha256, sessionID).
			Order("created_at asc").First(&preferred).Error; err2 == nil {
			return preferred, nil
		}
	}
	return row, err
}

// SoftDelete marks the row deleted; physical file unlink is the service's job.
func (r AttachmentRepository) SoftDelete(id string) error {
	now := time.Now().UTC()
	return r.db.Model(&model.Attachment{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", now).Error
}

// SoftDeleteBySession marks all of a session's attachments deleted. Used by
// session hard-delete cascade (docs/51 §10).
func (r AttachmentRepository) SoftDeleteBySession(sessionID string) (int64, error) {
	now := time.Now().UTC()
	result := r.db.Model(&model.Attachment{}).
		Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Update("deleted_at", now)
	return result.RowsAffected, result.Error
}

// ListOrphans returns soft-deleted attachments for file GC (docs/51 §10).
func (r AttachmentRepository) ListOrphans(limit int) ([]model.Attachment, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []model.Attachment
	err := r.db.Where("deleted_at IS NOT NULL").Limit(limit).Find(&rows).Error
	return rows, err
}

// DeletePermanently removes the row entirely (call after file unlink).
func (r AttachmentRepository) DeletePermanently(id string) error {
	return r.db.Where("id = ?", id).Delete(&model.Attachment{}).Error
}


// CopyToSession clones attachment metadata rows into targetSessionID, keeping
// the same StoragePath so both sessions share the on-disk bytes (docs/51 §10 /
// docs/52 Slice D fork strategy: shared storage_path, no file copy).
// Returns a map of source attachment id → new attachment id.
func (r AttachmentRepository) CopyToSession(sourceSessionID, targetSessionID string) (map[string]string, error) {
	rows, err := r.ListBySession(sourceSessionID)
	if err != nil {
		return nil, err
	}
	idMap := make(map[string]string, len(rows))
	now := time.Now().UTC()
	for i, row := range rows {
		oldID := row.ID
		row.ID = fmt.Sprintf("att_%d_%d", now.UnixNano(), i+1)
		row.SessionID = targetSessionID
		row.CreatedAt = now.Add(time.Duration(i) * time.Nanosecond)
		row.LastRefAt = now
		row.DeletedAt = nil
		if err := r.db.Create(&row).Error; err != nil {
			return idMap, err
		}
		idMap[oldID] = row.ID
	}
	return idMap, nil
}
