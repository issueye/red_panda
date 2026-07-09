package repository

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func TestMemoryRepositoryListUpdateDeleteAndSelect(t *testing.T) {
	repo := newMemoryTestRepository(t)
	project, err := repo.Create(model.MemoryRecord{
		Scope:         "project",
		Kind:          "fact",
		Status:        "active",
		Title:         "Project",
		Content:       "Project memory",
		Confidence:    "high",
		WorkspaceRoot: "D:/workspace",
		Source:        "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.Create(model.MemoryRecord{
		Scope:      "session",
		Kind:       "decision",
		Status:     "active",
		Title:      "Session",
		Content:    "Session memory",
		Confidence: "medium",
		SessionID:  "session_1",
		Source:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := repo.Create(model.MemoryRecord{
		Scope:      "session",
		Kind:       "fact",
		Status:     "disabled",
		Title:      "Disabled",
		Content:    "Disabled memory",
		Confidence: "medium",
		SessionID:  "session_1",
		Source:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.Create(model.MemoryRecord{
		Scope:         "project",
		Kind:          "fact",
		Status:        "active",
		Title:         "Other",
		Content:       "Other workspace memory",
		Confidence:    "high",
		WorkspaceRoot: "D:/other",
		Source:        "user",
	})
	if err != nil {
		t.Fatal(err)
	}

	active, err := repo.List(MemoryListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 3 {
		t.Fatalf("active list len = %d, want 3: %#v", len(active), active)
	}

	newTitle := "Project updated"
	newContent := "Updated project memory"
	newConfidence := "low"
	updated, err := repo.Update(MemoryUpdate{
		ID:         project.ID,
		Title:      &newTitle,
		Content:    &newContent,
		Confidence: &newConfidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != newTitle || updated.Content != newContent || updated.Confidence != newConfidence {
		t.Fatalf("update mismatch: %#v", updated)
	}

	deleted, err := repo.Delete(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != "deleted" || deleted.DeletedAt == nil {
		t.Fatalf("delete mismatch: %#v", deleted)
	}

	selected, err := repo.SelectForRun("session_1", "D:/workspace", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected len = %d, want 2: %#v", len(selected), selected)
	}
	if selected[0].ID != session.ID || selected[1].ID != project.ID {
		t.Fatalf("selection order/items mismatch: %#v", selected)
	}
	for _, row := range selected {
		if row.ID == disabled.ID || row.ID == other.ID {
			t.Fatalf("selected inactive or mismatched memory: %#v", selected)
		}
	}
}

func newMemoryTestRepository(t *testing.T) MemoryRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "memory.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.MemoryRecord{}); err != nil {
		t.Fatal(err)
	}
	return NewMemoryRepository(db)
}

func TestMemoryRepositoryDeletedCreateHasDeletedAt(t *testing.T) {
	repo := newMemoryTestRepository(t)
	row, err := repo.Create(model.MemoryRecord{
		Scope:      "session",
		Kind:       "warning",
		Status:     "deleted",
		Title:      "Deleted",
		Content:    "Deleted memory",
		Confidence: "low",
		SessionID:  "session_deleted",
		Source:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.DeletedAt == nil || time.Since(*row.DeletedAt) > time.Minute {
		t.Fatalf("deleted create missing deleted_at: %#v", row)
	}
}
