package repository

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func newContextTestRepository(t *testing.T) ContextRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "context.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.GoalNote{}); err != nil {
		t.Fatal(err)
	}
	return NewContextRepository(db)
}

func TestContextAppendAssignsMonotonicSeq(t *testing.T) {
	repo := newContextTestRepository(t)
	n1, recorded, err := repo.Append(model.GoalNote{GoalID: "g1", Kind: "finding", Title: "First", Body: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if !recorded || n1.Seq != 1 {
		t.Fatalf("first append: recorded=%v seq=%d", recorded, n1.Seq)
	}
	n2, recorded, err := repo.Append(model.GoalNote{GoalID: "g1", Kind: "finding", Title: "Second", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !recorded || n2.Seq != 2 {
		t.Fatalf("second append: recorded=%v seq=%d", recorded, n2.Seq)
	}
	// Separate goal restarts seq at 1.
	n3, _, err := repo.Append(model.GoalNote{GoalID: "g2", Kind: "finding", Title: "Other", Body: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if n3.Seq != 1 {
		t.Fatalf("other goal seq = %d, want 1", n3.Seq)
	}
}

func TestContextAppendIdempotentByID(t *testing.T) {
	repo := newContextTestRepository(t)
	row := model.GoalNote{ID: "note_fixed", GoalID: "g1", Kind: "finding", Title: "Once", Body: "a"}
	first, recorded, err := repo.Append(row)
	if err != nil {
		t.Fatal(err)
	}
	if !recorded {
		t.Fatal("first append should record")
	}
	second, recorded, err := repo.Append(row)
	if err != nil {
		t.Fatal(err)
	}
	if recorded {
		t.Fatal("duplicate append should not record")
	}
	if second.ID != first.ID || second.Seq != first.Seq {
		t.Fatalf("idempotent return mismatch: first=%#v second=%#v", first, second)
	}
}

func TestContextReplaceUpsertsByKindTitle(t *testing.T) {
	repo := newContextTestRepository(t)
	row := model.GoalNote{GoalID: "g1", Kind: "decision", Title: "Use SQLite", Body: "v1"}
	first, created, err := repo.Replace(row)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first replace should create")
	}
	row.Body = "v2-updated"
	updated, created, err := repo.Replace(row)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second replace should not create")
	}
	if updated.Body != "v2-updated" || updated.Seq != first.Seq {
		t.Fatalf("replace did not update in place: %#v", updated)
	}
}

func TestContextListPinnedFirstAndFilters(t *testing.T) {
	repo := newContextTestRepository(t)
	_, _, _ = repo.Append(model.GoalNote{GoalID: "g1", Kind: "fact", Title: "regular-1"})
	pinned, _, _ := repo.Append(model.GoalNote{GoalID: "g1", Kind: "fact", Title: "pinned-1"})
	_ = repo.db.Model(&model.GoalNote{}).Where("id = ?", pinned.ID).Update("pinned", true)
	_, _, _ = repo.Append(model.GoalNote{GoalID: "g1", Kind: "risk", Title: "regular-2"})

	// Default list: pinned first, then desc seq.
	rows, err := repo.List("g1", NoteListOpts{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Title != "pinned-1" {
		t.Fatalf("list order/contents wrong: %#v", rows)
	}
	// Kind filter.
	facts, err := repo.List("g1", NoteListOpts{Kind: "fact", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("kind filter count = %d, want 2", len(facts))
	}
	// Pinned-only filter.
	pinnedTrue := true
	pinnedRows, err := repo.List("g1", NoteListOpts{Pinned: &pinnedTrue, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(pinnedRows) != 1 {
		t.Fatalf("pinned filter count = %d, want 1", len(pinnedRows))
	}
}

func TestContextSearchMatchesTitleAndBody(t *testing.T) {
	repo := newContextTestRepository(t)
	_, _, _ = repo.Append(model.GoalNote{GoalID: "g1", Kind: "fact", Title: "Cache layer", Body: "uses redis"})
	_, _, _ = repo.Append(model.GoalNote{GoalID: "g1", Kind: "fact", Title: "Other", Body: "unrelated"})

	rows, err := repo.Search("g1", "redis", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Title != "Cache layer" {
		t.Fatalf("body search mismatch: %#v", rows)
	}
	rows, err = repo.Search("g1", "cache", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("title search mismatch: %#v", rows)
	}
	if _, err := repo.Search("g1", "   ", 10); err != nil {
		t.Fatalf("empty query should be a no-op, got %v", err)
	}
}

func TestContextDelete(t *testing.T) {
	repo := newContextTestRepository(t)
	n, _, err := repo.Append(model.GoalNote{GoalID: "g1", Kind: "fact", Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete("g1", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get("g1", n.ID); err == nil {
		t.Fatal("deleted note still retrievable")
	}
}
