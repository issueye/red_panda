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
