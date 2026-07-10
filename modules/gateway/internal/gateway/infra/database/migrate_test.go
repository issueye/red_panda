package database

import (
	"path/filepath"
	"testing"

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
