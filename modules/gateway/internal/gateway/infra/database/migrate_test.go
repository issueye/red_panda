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
	if !db.Migrator().HasIndex(&model.SessionCompaction{}, "idx_compactions_one_applied") {
		t.Fatal("active session summary unique index was not created")
	}
	first := model.SessionCompaction{ID: "c1", SourceSessionID: "s1", TargetSessionID: "s1", Status: "applied"}
	second := model.SessionCompaction{ID: "c2", SourceSessionID: "s1", TargetSessionID: "s1", Status: "applied"}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err == nil {
		t.Fatal("multiple active summaries were accepted for one session")
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

func TestMigrateRepairsDuplicateMessageSequencesBeforeAddingUniqueIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	type legacyMessage struct {
		ID          string `gorm:"primaryKey"`
		SessionID   string
		Role        string
		ContentJSON string
		Seq         uint64
		CreatedAt   time.Time
	}
	if err := db.Table("messages").AutoMigrate(&legacyMessage{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacy := []legacyMessage{
		{ID: "m1", SessionID: "s1", Role: "user", ContentJSON: `[]`, Seq: 1, CreatedAt: now},
		{ID: "m2", SessionID: "s1", Role: "assistant", ContentJSON: `[]`, Seq: 1, CreatedAt: now.Add(time.Second)},
	}
	if err := db.Table("messages").Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasIndex(&model.Message{}, "idx_messages_session_seq") {
		t.Fatal("message sequence unique index was not created")
	}
	var rows []model.Message
	if err := db.Where("session_id = ?", "s1").Order("seq asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Seq != 1 || rows[1].Seq != 2 {
		t.Fatalf("repaired message sequences = %#v", rows)
	}
}
