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
		ID: "provider_legacy", Name: "legacy", Provider: "openai_responses", BaseURL: "https://example.test", Active: true,
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
	if profile.CacheMode != "implicit" {
		t.Fatalf("existing Responses profile cache_mode = %q, want implicit", profile.CacheMode)
	}
	if !profile.CacheKeySupported || profile.CacheKeyConfigured {
		t.Fatalf("existing Responses profile cache key defaults = supported:%v configured:%v", profile.CacheKeySupported, profile.CacheKeyConfigured)
	}
	// http_proxy 列在 Migrate 中为老库补建,默认空串(回退到环境代理)。
	if profile.HTTPProxy != "" {
		t.Fatalf("existing provider profile http_proxy should default to empty, got %q", profile.HTTPProxy)
	}
	if !db.Migrator().HasColumn(&model.ProviderProfile{}, "http_proxy") {
		t.Fatal("provider_profiles.http_proxy column was not created")
	}
}

func TestMigrateBackfillsPromptCacheMetadata(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.ProviderProfile{}, &model.SessionCompaction{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProviderProfile{
		ID: "provider_anthropic", Name: "legacy anthropic", Provider: "anthropic", Active: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SessionCompaction{
		ID: "compaction_legacy", SourceSessionID: "session_legacy", TargetSessionID: "session_legacy",
		Status: "applied", SourceStartSeq: 1, SourceEndSeq: 20, SummaryJSON: `{"summary":"stable"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var profile model.ProviderProfile
	if err := db.First(&profile, "id = ?", "provider_anthropic").Error; err != nil {
		t.Fatal(err)
	}
	if profile.CacheMode != "explicit" {
		t.Fatalf("anthropic cache_mode = %q, want explicit", profile.CacheMode)
	}
	var compaction model.SessionCompaction
	if err := db.First(&compaction, "id = ?", "compaction_legacy").Error; err != nil {
		t.Fatal(err)
	}
	if compaction.SummaryDigest == "" || compaction.PromptSchemaVersion == "" || compaction.CacheEpoch == "" {
		t.Fatalf("cache metadata was not backfilled: %#v", compaction)
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
