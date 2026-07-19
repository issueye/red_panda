package repository

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func TestProviderProfileRepositoryDefaultIsExclusive(t *testing.T) {
	repo := newProviderProfileTestRepository(t)
	first, err := repo.Create(model.ProviderProfile{
		Name:         "first",
		Provider:     "openai_compatible",
		BaseURL:      "https://first.invalid",
		Model:        "first-model",
		APIKeySecret: "sk-first",
		IsDefault:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(model.ProviderProfile{
		Name:      "second",
		Provider:  "openai_compatible",
		BaseURL:   "https://second.invalid",
		Model:     "second-model",
		IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstAfter, err := repo.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstAfter.IsDefault {
		t.Fatalf("expected first profile default to be cleared: %#v", firstAfter)
	}
	currentDefault, err := repo.Default()
	if err != nil {
		t.Fatal(err)
	}
	if currentDefault.ID != second.ID {
		t.Fatalf("default profile = %s, want %s", currentDefault.ID, second.ID)
	}
}

func TestProviderProfileRepositoryDeleteHidesProfile(t *testing.T) {
	repo := newProviderProfileTestRepository(t)
	profile, err := repo.Create(model.ProviderProfile{
		Name:      "delete-me",
		Provider:  "openai_compatible",
		BaseURL:   "https://delete.invalid",
		IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(profile.ID); err != gorm.ErrRecordNotFound {
		t.Fatalf("expected record not found after delete, got %v", err)
	}
	rows, err := repo.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected deleted profile hidden from list, got %#v", rows)
	}
}

func TestProviderProfileRepositoryPersistsMultipleModels(t *testing.T) {
	repo := newProviderProfileTestRepository(t)
	profile, err := repo.Create(model.ProviderProfile{
		Name: "multi", Provider: "openai_responses", BaseURL: "https://example.invalid/v1",
		Model: "gpt-fast", MaxTokens: 128000,
		Models: []model.ProviderModel{
			{Model: "gpt-fast", Label: "Fast", MaxTokens: 128000, ReasoningEffort: "low"},
			{Model: "gpt-deep", Label: "Deep", MaxTokens: 200000, ReasoningEffort: "high"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Models) != 2 || stored.Models[1].Model != "gpt-deep" || stored.Models[1].ReasoningEffort != "high" {
		t.Fatalf("created models = %#v", stored.Models)
	}
	stored.Model = "gpt-deep"
	stored.MaxTokens = 200000
	stored.Models[0].Label = "Quick"
	if _, err := repo.Update(stored); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.Get(profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Models) != 2 || updated.Models[0].Label != "Quick" || updated.Model != "gpt-deep" {
		t.Fatalf("updated profile = %#v", updated)
	}
}

func newProviderProfileTestRepository(t *testing.T) ProviderProfileRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "providers.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.ProviderProfile{}); err != nil {
		t.Fatal(err)
	}
	return NewProviderProfileRepository(db)
}
