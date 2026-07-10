package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

var ErrMCPServerNameExists = errors.New("MCP server name already exists")

type MCPServerConfigRepository struct {
	db *gorm.DB
}

type MCPServerConfigUpdate struct {
	ID            string
	Name          *string
	Command       *string
	Args          *[]string
	Env           *map[string]string
	CWD           *string
	Enabled       *bool
	Timeouts      *model.MCPTimeouts
	ToolAllowlist *[]string
	RiskOverrides *map[string]string
}

func NewMCPServerConfigRepository(db *gorm.DB) MCPServerConfigRepository {
	return MCPServerConfigRepository{db: db}
}

func (r MCPServerConfigRepository) Create(config model.MCPServerConfig) (model.MCPServerConfig, error) {
	exists, err := r.nameExists(config.Name, "")
	if err != nil {
		return model.MCPServerConfig{}, err
	}
	if exists {
		return model.MCPServerConfig{}, ErrMCPServerNameExists
	}

	now := time.Now().UTC()
	if config.ID == "" {
		config.ID = fmt.Sprintf("mcp_srv_%d", now.UnixNano())
	}
	config.CreatedAt = now
	config.UpdatedAt = now
	config.DeletedAt = nil
	return config, r.db.Create(&config).Error
}

func (r MCPServerConfigRepository) Get(id string) (model.MCPServerConfig, error) {
	var row model.MCPServerConfig
	err := r.db.First(&row, "id = ? AND deleted_at IS NULL", id).Error
	return row, err
}

func (r MCPServerConfigRepository) GetByName(name string) (model.MCPServerConfig, error) {
	var row model.MCPServerConfig
	err := r.db.First(&row, "name = ? AND deleted_at IS NULL", name).Error
	return row, err
}

func (r MCPServerConfigRepository) List(limit int) ([]model.MCPServerConfig, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []model.MCPServerConfig
	err := r.db.
		Where("deleted_at IS NULL").
		Order("updated_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r MCPServerConfigRepository) Update(input MCPServerConfigUpdate) (model.MCPServerConfig, error) {
	current, err := r.Get(input.ID)
	if err != nil {
		return model.MCPServerConfig{}, err
	}
	if input.Name != nil && *input.Name != current.Name {
		exists, err := r.nameExists(*input.Name, current.ID)
		if err != nil {
			return model.MCPServerConfig{}, err
		}
		if exists {
			return model.MCPServerConfig{}, ErrMCPServerNameExists
		}
		current.Name = *input.Name
	}
	if input.Command != nil {
		current.Command = *input.Command
	}
	if input.Args != nil {
		current.Args = *input.Args
	}
	if input.Env != nil {
		current.Env = *input.Env
	}
	if input.CWD != nil {
		current.CWD = *input.CWD
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if input.Timeouts != nil {
		current.Timeouts = *input.Timeouts
	}
	if input.ToolAllowlist != nil {
		current.ToolAllowlist = *input.ToolAllowlist
	}
	if input.RiskOverrides != nil {
		current.RiskOverrides = *input.RiskOverrides
	}
	current.UpdatedAt = time.Now().UTC()
	return current, r.db.Save(&current).Error
}

func (r MCPServerConfigRepository) Delete(id string) error {
	now := time.Now().UTC()
	result := r.db.Model(&model.MCPServerConfig{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"enabled":    false,
			"deleted_at": &now,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r MCPServerConfigRepository) nameExists(name string, excludeID string) (bool, error) {
	query := r.db.Model(&model.MCPServerConfig{}).
		Where("name = ? AND deleted_at IS NULL", name)
	if excludeID != "" {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}
