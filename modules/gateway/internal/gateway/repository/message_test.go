package repository

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

func TestMessageRepositoryAddOrAppendAggregatesConsecutiveRunDeltas(t *testing.T) {
	repo := newMessageTestRepository(t)
	if _, err := repo.Add("session_1", "user", "hello", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "assistant", "first ", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "assistant", "second", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "subagent", "plan", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "assistant", "new run", "run_2"); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.List("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	assertMessageText(t, rows[0], "hello")
	assertMessageText(t, rows[1], "first second")
	assertMessageText(t, rows[2], "plan")
	assertMessageText(t, rows[3], "new run")
	if rows[1].Role != "assistant" || rows[1].RunID != "run_1" {
		t.Fatalf("assistant aggregate metadata mismatch: %#v", rows[1])
	}
	if rows[2].Role != "subagent" || rows[2].RunID != "run_1" {
		t.Fatalf("subagent metadata mismatch: %#v", rows[2])
	}
	if rows[3].Role != "assistant" || rows[3].RunID != "run_2" {
		t.Fatalf("new run metadata mismatch: %#v", rows[3])
	}
}

func TestMessageRepositoryAddOrAppendAggregatesConsecutiveSubagentDeltas(t *testing.T) {
	repo := newMessageTestRepository(t)
	if _, err := repo.AddOrAppend("session_1", "subagent", "plan ", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "subagent", "step", "run_1"); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.List("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Role != "subagent" || rows[0].RunID != "run_1" {
		t.Fatalf("subagent aggregate metadata mismatch: %#v", rows[0])
	}
	assertMessageText(t, rows[0], "plan step")
}

func TestMessageRepositoryAddOrAppendDoesNotAppendUserMessages(t *testing.T) {
	repo := newMessageTestRepository(t)
	if _, err := repo.AddOrAppend("session_1", "user", "first", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddOrAppend("session_1", "user", "second", "run_1"); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.List("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	assertMessageText(t, rows[0], "first")
	assertMessageText(t, rows[1], "second")
}

func newMessageTestRepository(t *testing.T) MessageRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "messages.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.Message{}); err != nil {
		t.Fatal(err)
	}
	return NewMessageRepository(db)
}

func assertMessageText(t *testing.T, row model.Message, want string) {
	t.Helper()
	var content []methods.ContentBlock
	if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Text != want {
		t.Fatalf("message text = %#v, want %q", content, want)
	}
}
