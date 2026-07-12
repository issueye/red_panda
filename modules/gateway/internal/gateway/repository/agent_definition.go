package repository

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

type AgentDefinitionRepository struct {
	db *gorm.DB
}

func NewAgentDefinitionRepository(db *gorm.DB) AgentDefinitionRepository {
	return AgentDefinitionRepository{db: db}
}

func (r AgentDefinitionRepository) Create(row model.AgentDefinition) (model.AgentDefinition, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(row.ID) == "" {
		row.ID = fmt.Sprintf("agent_%d", now.UnixNano())
	}
	row.Key = strings.TrimSpace(row.Key)
	if row.Key == "" {
		return model.AgentDefinition{}, fmt.Errorf("key is required")
	}
	if strings.TrimSpace(row.Name) == "" {
		row.Name = row.Key
	}
	if row.Kind == "" {
		row.Kind = "custom"
	}
	if row.Phase == "" {
		row.Phase = "custom"
	}
	row.CreatedAt = now
	row.UpdatedAt = now
	row.DeletedAt = nil
	if err := r.db.Create(&row).Error; err != nil {
		return model.AgentDefinition{}, err
	}
	return row, nil
}

func (r AgentDefinitionRepository) UpsertByKey(row model.AgentDefinition) (model.AgentDefinition, error) {
	row.Key = strings.TrimSpace(row.Key)
	if row.Key == "" {
		return model.AgentDefinition{}, fmt.Errorf("key is required")
	}
	var current model.AgentDefinition
	err := r.db.Where("key = ? AND deleted_at IS NULL", row.Key).First(&current).Error
	if err == gorm.ErrRecordNotFound {
		return r.Create(row)
	}
	if err != nil {
		return model.AgentDefinition{}, err
	}
	// Preserve user toggles on re-seed of builtins.
	if row.Builtin {
		row.ID = current.ID
		row.Enabled = current.Enabled
		row.CreatedAt = current.CreatedAt
		// Allow user-edited prompt/turns to stick if already customized? For seed: only fill empty custom fields.
		if strings.TrimSpace(current.SystemPrompt) != "" && current.SystemPrompt != row.SystemPrompt {
			// Keep operator overrides for prompt/max turns when they differ from seed.
			row.SystemPrompt = current.SystemPrompt
			row.DefaultMaxTurns = current.DefaultMaxTurns
			row.Description = current.Description
			row.Name = current.Name
			row.NameZH = current.NameZH
			row.ToolAllowlist = current.ToolAllowlist
			row.ToolDenylist = current.ToolDenylist
		}
	} else {
		row.ID = current.ID
		row.CreatedAt = current.CreatedAt
	}
	row.UpdatedAt = time.Now().UTC()
	if err := r.db.Save(&row).Error; err != nil {
		return model.AgentDefinition{}, err
	}
	return row, nil
}

func (r AgentDefinitionRepository) Update(row model.AgentDefinition) (model.AgentDefinition, error) {
	var current model.AgentDefinition
	if err := r.db.First(&current, "id = ? AND deleted_at IS NULL", row.ID).Error; err != nil {
		return model.AgentDefinition{}, err
	}
	current.Name = row.Name
	current.NameZH = row.NameZH
	current.Phase = row.Phase
	current.Description = row.Description
	current.SystemPrompt = row.SystemPrompt
	current.ToolAllowlist = row.ToolAllowlist
	current.ToolDenylist = row.ToolDenylist
	current.DefaultMaxTurns = row.DefaultMaxTurns
	current.Enabled = row.Enabled
	current.SortOrder = row.SortOrder
	current.MetadataJSON = row.MetadataJSON
	// Key/Kind/Builtin immutable after create for safety.
	current.UpdatedAt = time.Now().UTC()
	if err := r.db.Save(&current).Error; err != nil {
		return model.AgentDefinition{}, err
	}
	return current, nil
}

func (r AgentDefinitionRepository) Get(id string) (model.AgentDefinition, error) {
	var row model.AgentDefinition
	err := r.db.First(&row, "id = ? AND deleted_at IS NULL", id).Error
	return row, err
}

func (r AgentDefinitionRepository) GetByKey(key string) (model.AgentDefinition, error) {
	var row model.AgentDefinition
	err := r.db.First(&row, "key = ? AND deleted_at IS NULL", strings.TrimSpace(key)).Error
	return row, err
}

func (r AgentDefinitionRepository) List(limit int) ([]model.AgentDefinition, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.AgentDefinition
	err := r.db.Where("deleted_at IS NULL").
		Order("sort_order asc, created_at asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r AgentDefinitionRepository) ListEnabled(limit int) ([]model.AgentDefinition, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []model.AgentDefinition
	err := r.db.Where("deleted_at IS NULL AND enabled = ?", true).
		Order("sort_order asc, created_at asc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r AgentDefinitionRepository) SoftDelete(id string) error {
	var current model.AgentDefinition
	if err := r.db.First(&current, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
		return err
	}
	if current.Builtin {
		return fmt.Errorf("builtin agent %q cannot be deleted", current.Key)
	}
	now := time.Now().UTC()
	return r.db.Model(&model.AgentDefinition{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"deleted_at": now,
			"updated_at": now,
		}).Error
}
