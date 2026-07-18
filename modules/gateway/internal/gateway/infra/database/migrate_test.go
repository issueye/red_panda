package database

import (
	"path/filepath"
	"testing"
	"time"

	"redpanda/gateway/internal/gateway/model"
)

func TestMigrateCreatesMCPServerConfigSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&model.MCPServerConfig{}) {
		t.Fatal("MCP server config table was not created")
	}
	for _, column := range []string{
		"args_json",
		"env_json",
		"timeouts_json",
		"tool_allowlist_json",
		"risk_overrides_json",
	} {
		if !db.Migrator().HasColumn(&model.MCPServerConfig{}, column) {
			t.Fatalf("MCP server config column %q was not created", column)
		}
	}
}

func TestMigrateDefaultsExistingProviderProfilesToStreaming(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	type legacyProviderProfile struct {
		ID        string `gorm:"primaryKey"`
		Name      string
		Provider  string
		BaseURL   string
		Active    bool
		CreatedAt time.Time
		UpdatedAt time.Time
	}
	if err := db.Table("provider_profiles").AutoMigrate(&legacyProviderProfile{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("provider_profiles").Create(&legacyProviderProfile{
		ID: "provider_legacy", Name: "legacy", Provider: "openai_compatible", BaseURL: "https://example.test", Active: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var profile model.ProviderProfile
	if err := db.First(&profile, "id = ?", "provider_legacy").Error; err != nil {
		t.Fatal(err)
	}
	if !profile.Stream {
		t.Fatal("existing provider profile should default to streaming")
	}
}
