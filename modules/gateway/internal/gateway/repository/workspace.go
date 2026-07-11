package repository

import (
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type WorkspaceRepository struct {
	db *gorm.DB
}

func NewWorkspaceRepository(db *gorm.DB) WorkspaceRepository {
	return WorkspaceRepository{db: db}
}

func (r WorkspaceRepository) Upsert(root string) (model.Workspace, error) {
	now := time.Now().UTC()
	workspace := model.Workspace{
		ID:           "ws_" + stableID(root),
		Root:         root,
		Name:         filepath.Base(root),
		LastOpenedAt: now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := r.db.
		Where("root = ?", root).
		Assign(model.Workspace{Name: workspace.Name, LastOpenedAt: now, UpdatedAt: now}).
		FirstOrCreate(&workspace).Error
	return workspace, err
}

func (r WorkspaceRepository) Current() (model.Workspace, bool, error) {
	var workspace model.Workspace
	err := r.db.Order("last_opened_at desc").First(&workspace).Error
	if err == gorm.ErrRecordNotFound {
		return model.Workspace{}, false, nil
	}
	return workspace, err == nil, err
}

func (r WorkspaceRepository) Recent(limit int) ([]model.Workspace, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []model.Workspace
	err := r.db.Order("last_opened_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// Get returns a workspace by id.
func (r WorkspaceRepository) Get(id string) (model.Workspace, error) {
	var workspace model.Workspace
	err := r.db.Where("id = ?", id).First(&workspace).Error
	return workspace, err
}

// Delete removes a workspace from the recent list.
func (r WorkspaceRepository) Delete(id string) error {
	result := r.db.Where("id = ?", id).Delete(&model.Workspace{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
