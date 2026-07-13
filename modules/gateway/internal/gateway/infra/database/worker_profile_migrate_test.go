package database

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func openWorkerProfileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestMigrateWorkerProfilesEmptyDatabase(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&model.WorkerProfile{}) {
		t.Fatal("worker_profiles table was not created")
	}
	if db.Migrator().HasTable("agent_definitions") {
		t.Fatal("agent_definitions legacy table was not removed")
	}
	for _, column := range []string{"provider", "model", "tool_allowlist_json", "tool_denylist_json"} {
		if !db.Migrator().HasColumn(&model.WorkerProfile{}, column) {
			t.Fatalf("worker profile column %q was not created", column)
		}
	}
	var count int64
	if err := db.Model(&model.WorkerProfile{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no worker profiles, got %d", count)
	}
}

func TestMigrateWorkerProfilesCopiesAgentDefinitions(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	source := legacyAgentDefinition{
		ID: "agent_existing", Key: "reviewer", Name: "Reviewer", NameZH: "审阅者",
		Kind: "custom", Phase: "verify", Description: "Review changes",
		SystemPrompt:  "Inspect the implementation.",
		ToolAllowlist: []string{"workspace.read_file", "workspace.grep"},
		ToolDenylist:  []string{"workspace.write_file"}, DefaultMaxTurns: 9,
		Enabled: true, SortOrder: 30, MetadataJSON: `{"source":"test"}`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var got model.WorkerProfile
	if err := db.Where("key = ?", source.Key).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ID != "worker_profile_"+source.ID || got.Name != source.Name || got.NameZH != source.NameZH ||
		got.Kind != source.Kind || got.Phase != source.Phase || got.Description != source.Description ||
		got.SystemPrompt != source.SystemPrompt || got.DefaultMaxTurns != source.DefaultMaxTurns ||
		got.Enabled != source.Enabled || got.Builtin != source.Builtin || got.SortOrder != source.SortOrder ||
		got.MetadataJSON != source.MetadataJSON || !reflect.DeepEqual(got.ToolAllowlist, source.ToolAllowlist) ||
		!reflect.DeepEqual(got.ToolDenylist, source.ToolDenylist) {
		t.Fatalf("worker profile did not preserve source fields:\nsource=%+v\ngot=%+v", source, got)
	}
}

func TestMigrateWorkerProfilesIsIdempotent(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyAgentDefinition{
		ID: "agent_repeat", Key: "repeat", Name: "Repeat", Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.WorkerProfile{}).Where("key = ?", "repeat").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one migrated profile, got %d", count)
	}
}

func TestMigrateWorkerProfilesPreservesExistingCustomization(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}, &model.WorkerProfile{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacyAgentDefinition{
		ID: "agent_customized", Key: "customized", SystemPrompt: "old prompt",
		ToolAllowlist: []string{"workspace.read_file"}, ToolDenylist: []string{"shell.exec"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	existing := model.WorkerProfile{
		ID: "worker_profile_customized", Key: "customized", SystemPrompt: "operator prompt",
		Provider: "openai_compatible", Model: "operator-model",
		ToolAllowlist: []string{"workspace.grep"}, ToolDenylist: []string{"workspace.write_file"},
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var got model.WorkerProfile
	if err := db.Where("key = ?", existing.Key).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ID != existing.ID || got.SystemPrompt != existing.SystemPrompt ||
		got.Provider != existing.Provider || got.Model != existing.Model ||
		!reflect.DeepEqual(got.ToolAllowlist, existing.ToolAllowlist) ||
		!reflect.DeepEqual(got.ToolDenylist, existing.ToolDenylist) {
		t.Fatalf("existing worker profile customization was overwritten: %+v", got)
	}
}

func TestMigrateWorkerProfilesCopiesActiveDefinitionAfterDeletedSameKey(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}); err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC().Add(-time.Hour)
	deleted := legacyAgentDefinition{
		ID: "agent_deleted", Key: "reused", Name: "Deleted",
		CreatedAt: deletedAt.Add(-time.Hour), UpdatedAt: deletedAt, DeletedAt: &deletedAt,
	}
	active := legacyAgentDefinition{
		ID: "agent_active", Key: "reused", Name: "Active", Enabled: true,
		CreatedAt: deletedAt.Add(time.Hour), UpdatedAt: deletedAt.Add(time.Hour),
	}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	var got model.WorkerProfile
	if err := db.Where("key = ? AND deleted_at IS NULL", active.Key).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ID != "worker_profile_"+active.ID || got.Name != active.Name || !got.Enabled {
		t.Fatalf("expected active definition to be migrated, got %+v", got)
	}
	var count int64
	if err := db.Model(&model.WorkerProfile{}).Where("key = ?", active.Key).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected deleted source to be ignored, got %d profiles", count)
	}
}

func TestMigrateWorkerProfilesReplacesDeletedTargetWithActiveProfile(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}, &model.WorkerProfile{}); err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC().Add(-time.Hour)
	source := legacyAgentDefinition{ID: "agent_replacement", Key: "replacement", Name: "Current", Enabled: true}
	deletedTarget := model.WorkerProfile{
		ID: "worker_profile_" + source.ID, Key: source.Key, Name: "Old", DeletedAt: &deletedAt,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&deletedTarget).Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	var got model.WorkerProfile
	if err := db.Where("key = ? AND deleted_at IS NULL", source.Key).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.ID != "worker_profile_"+source.ID+"_2" || got.Name != source.Name {
		t.Fatalf("expected a new active profile with a conflict-free ID, got %+v", got)
	}
}

func TestMigrateWorkerProfilesAvoidsIDConflictWithDifferentKey(t *testing.T) {
	db := openWorkerProfileTestDB(t)
	if err := db.AutoMigrate(&legacyAgentDefinition{}, &model.WorkerProfile{}); err != nil {
		t.Fatal(err)
	}
	source := legacyAgentDefinition{ID: "agent_collision", Key: "migrated", Name: "Migrated", Enabled: true}
	existing := model.WorkerProfile{ID: "worker_profile_" + source.ID, Key: "existing", Name: "Existing"}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	var migrated model.WorkerProfile
	if err := db.Where("key = ? AND deleted_at IS NULL", source.Key).First(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.ID != "worker_profile_"+source.ID+"_2" || migrated.Name != source.Name {
		t.Fatalf("expected source under a conflict-free ID, got %+v", migrated)
	}
	var preserved model.WorkerProfile
	if err := db.First(&preserved, "id = ?", existing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.Key != existing.Key || preserved.Name != existing.Name {
		t.Fatalf("existing profile was changed: %+v", preserved)
	}
}
