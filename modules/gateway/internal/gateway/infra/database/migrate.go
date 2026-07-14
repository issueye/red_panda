package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(
			&model.Session{},
			&model.SessionLineage{},
			&model.SessionCompaction{},
			&model.MemoryRecord{},
			&model.TodoItem{},
			&model.Message{},
			&model.RunRecord{},
			&model.RunEvent{},
			&model.ToolCall{},
			&model.Workspace{},
			&model.PermissionRequest{},
			&model.ProviderProfile{},
			&model.MCPServerConfig{},
			&model.WorkerProfile{},
			&model.Goal{},
			&model.GoalSegment{},
			&model.GoalNote{},
			&model.GoalAction{},
			&model.GoalEvent{},
		); err != nil {
			return err
		}
		if err := migrateWorkerProfiles(tx); err != nil {
			return err
		}
		return tx.Migrator().DropTable("agent_definitions")
	})
}

type legacyAgentDefinition struct {
	ID              string `gorm:"primaryKey"`
	Key             string
	Name            string
	NameZH          string
	Kind            string
	Phase           string
	Description     string
	SystemPrompt    string
	ToolAllowlist   []string `gorm:"column:tool_allowlist_json;serializer:json;type:text"`
	ToolDenylist    []string `gorm:"column:tool_denylist_json;serializer:json;type:text"`
	DefaultMaxTurns int
	Enabled         bool
	Builtin         bool
	SortOrder       int
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

func (legacyAgentDefinition) TableName() string { return "agent_definitions" }

func migrateWorkerProfiles(db *gorm.DB) error {
	if !db.Migrator().HasTable("agent_definitions") {
		return nil
	}
	var definitions []legacyAgentDefinition
	if err := db.Where("deleted_at IS NULL").
		Order("created_at asc, id asc").
		Find(&definitions).Error; err != nil {
		return err
	}
	for _, definition := range definitions {
		var count int64
		if err := db.Model(&model.WorkerProfile{}).
			Where("key = ? AND deleted_at IS NULL", definition.Key).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			continue
		}
		profileID, err := nextWorkerProfileID(db, definition.ID)
		if err != nil {
			return err
		}
		profile := model.WorkerProfile{
			ID:              profileID,
			Key:             definition.Key,
			Name:            definition.Name,
			NameZH:          definition.NameZH,
			Kind:            definition.Kind,
			Phase:           definition.Phase,
			Description:     definition.Description,
			SystemPrompt:    definition.SystemPrompt,
			ToolAllowlist:   definition.ToolAllowlist,
			ToolDenylist:    definition.ToolDenylist,
			DefaultMaxTurns: definition.DefaultMaxTurns,
			Enabled:         definition.Enabled,
			Builtin:         definition.Builtin,
			SortOrder:       definition.SortOrder,
			MetadataJSON:    definition.MetadataJSON,
			CreatedAt:       definition.CreatedAt,
			UpdatedAt:       definition.UpdatedAt,
		}
		if err := db.Create(&profile).Error; err != nil {
			return err
		}
	}
	return nil
}

func nextWorkerProfileID(db *gorm.DB, sourceID string) (string, error) {
	base := "worker_profile_" + sourceID
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s_%d", base, suffix)
		}
		var count int64
		if err := db.Model(&model.WorkerProfile{}).
			Where("id = ?", candidate).
			Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}
