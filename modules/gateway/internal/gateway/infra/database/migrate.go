package database

import (
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Session{},
		&model.SessionLineage{},
		&model.SessionCompaction{},
		&model.MemoryRecord{},
		&model.Message{},
		&model.RunRecord{},
		&model.RunEvent{},
		&model.ToolCall{},
		&model.Workspace{},
		&model.PermissionRequest{},
		&model.ProviderProfile{},
		&model.MCPServerConfig{},
	)
}
