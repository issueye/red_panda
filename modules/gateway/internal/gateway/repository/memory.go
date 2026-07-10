package repository

import (
	"fmt"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

var memoryIDCounter atomic.Uint64

type MemoryRepository struct {
	db *gorm.DB
}

type MemoryListFilter struct {
	Scope         string
	WorkspaceRoot string
	SessionID     string
	Status        string
	Limit         int
}

type MemoryUpdate struct {
	ID           string
	Scope        *string
	Kind         *string
	Status       *string
	Title        *string
	Content      *string
	Confidence   *string
	MetadataJSON *string
}

func NewMemoryRepository(db *gorm.DB) MemoryRepository {
	return MemoryRepository{db: db}
}

func (r MemoryRepository) Create(record model.MemoryRecord) (model.MemoryRecord, error) {
	now := time.Now().UTC()
	if record.ID == "" {
		record.ID = fmt.Sprintf("mem_%d_%d", now.UnixNano(), memoryIDCounter.Add(1))
	}
	if record.Status == "" {
		record.Status = "active"
	}
	if record.Status == "deleted" && record.DeletedAt == nil {
		record.DeletedAt = &now
	}
	record.CreatedAt = now
	record.UpdatedAt = now
	return record, r.db.Create(&record).Error
}

func (r MemoryRepository) Get(id string) (model.MemoryRecord, error) {
	var row model.MemoryRecord
	err := r.db.First(&row, "id = ?", id).Error
	return row, err
}

func (r MemoryRepository) List(filter MemoryListFilter) ([]model.MemoryRecord, error) {
	limit := normalizeMemoryLimit(filter.Limit)
	query := r.db.Model(&model.MemoryRecord{})
	if filter.Status == "" {
		query = query.Where("status = ? AND deleted_at IS NULL", "active")
	} else if filter.Status != "all" {
		query = query.Where("status = ?", filter.Status)
		if filter.Status != "deleted" {
			query = query.Where("deleted_at IS NULL")
		}
	}
	if filter.Scope != "" {
		query = query.Where("scope = ?", filter.Scope)
	}
	if filter.WorkspaceRoot != "" {
		query = query.Where("workspace_root = ?", filter.WorkspaceRoot)
	}
	if filter.SessionID != "" {
		query = query.Where("session_id = ?", filter.SessionID)
	}
	var rows []model.MemoryRecord
	err := query.Order("updated_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r MemoryRepository) Update(input MemoryUpdate) (model.MemoryRecord, error) {
	current, err := r.Get(input.ID)
	if err != nil {
		return model.MemoryRecord{}, err
	}
	if input.Scope != nil {
		current.Scope = *input.Scope
	}
	if input.Kind != nil {
		current.Kind = *input.Kind
	}
	if input.Status != nil {
		current.Status = *input.Status
		if current.Status == "deleted" && current.DeletedAt == nil {
			now := time.Now().UTC()
			current.DeletedAt = &now
		}
		if current.Status != "deleted" {
			current.DeletedAt = nil
		}
	}
	if input.Title != nil {
		current.Title = *input.Title
	}
	if input.Content != nil {
		current.Content = *input.Content
	}
	if input.Confidence != nil {
		current.Confidence = *input.Confidence
	}
	if input.MetadataJSON != nil {
		current.MetadataJSON = *input.MetadataJSON
	}
	current.UpdatedAt = time.Now().UTC()
	return current, r.db.Save(&current).Error
}

func (r MemoryRepository) Delete(id string) (model.MemoryRecord, error) {
	now := time.Now().UTC()
	if err := r.db.Model(&model.MemoryRecord{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"status":     "deleted",
			"deleted_at": &now,
			"updated_at": now,
		}).Error; err != nil {
		return model.MemoryRecord{}, err
	}
	return r.Get(id)
}

func (r MemoryRepository) SelectForRun(sessionID string, workspaceRoot string, limit int) ([]model.MemoryRecord, error) {
	limit = normalizeMemoryLimit(limit)
	query := r.db.Model(&model.MemoryRecord{}).
		Where("status = ? AND deleted_at IS NULL", "active")
	if sessionID != "" && workspaceRoot != "" {
		query = query.Where("(scope = ? AND session_id = ?) OR (scope = ? AND workspace_root = ?)", "session", sessionID, "project", workspaceRoot)
	} else if sessionID != "" {
		query = query.Where("scope = ? AND session_id = ?", "session", sessionID)
	} else if workspaceRoot != "" {
		query = query.Where("scope = ? AND workspace_root = ?", "project", workspaceRoot)
	} else {
		return nil, nil
	}
	var rows []model.MemoryRecord
	err := query.
		Order(scopePrioritySQL()).
		Order("updated_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func normalizeMemoryLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 100
	}
	return limit
}

func scopePrioritySQL() string {
	return "CASE scope WHEN 'session' THEN 0 WHEN 'project' THEN 1 WHEN 'user' THEN 2 ELSE 3 END"
}
