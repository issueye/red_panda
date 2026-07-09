package repository

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
)

func TestRunRecordProjectionDoesNotOverwriteStartMetadata(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	started := time.Now().UTC()
	if err := repo.ProjectEvent(events.Envelope{
		EventID:   "evt_run_1_1",
		RootRunID: "run_1",
		SessionID: "session_1",
		RootSeq:   1,
		Type:      events.EventMessageDelta,
		Payload:   map[string]any{"delta": "early"},
		CreatedAt: started,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(model.RunRecord{
		ID:            "run_1",
		SessionID:     "session_1",
		WorkspaceRoot: "D:/workspace",
		RuntimeMode:   "per_run_process",
		Status:        "running",
		Input:         "/read README.md",
		StartedAt:     started.Add(time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProjectEvent(events.Envelope{
		EventID:   "evt_run_1_2",
		RootRunID: "run_1",
		SessionID: "session_1",
		RootSeq:   2,
		Type:      events.EventFinish,
		Payload:   map[string]any{"status": "completed"},
		CreatedAt: started.Add(2 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Get("run_1")
	if err != nil {
		t.Fatal(err)
	}
	if row.RuntimeMode != "per_run_process" || row.WorkspaceRoot != "D:/workspace" || row.Input != "/read README.md" {
		t.Fatalf("start metadata was overwritten: %#v", row)
	}
	if row.Status != "completed" || row.LastRootSeq != 2 {
		t.Fatalf("event projection mismatch: %#v", row)
	}
}

func TestRunRecordProjectionDoesNotOverwriteEventCounts(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	now := time.Now().UTC()
	if err := repo.Start(model.RunRecord{
		ID:        "run_counts",
		SessionID: "session_1",
		Status:    "running",
		StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProjectEvent(events.Envelope{
		EventID:   "evt_tool",
		RootRunID: "run_counts",
		SessionID: "session_1",
		RootSeq:   1,
		Type:      events.EventToolStarted,
		CreatedAt: now.Add(time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProjectEvent(events.Envelope{
		EventID:   "evt_finish",
		RootRunID: "run_counts",
		SessionID: "session_1",
		RootSeq:   2,
		Type:      events.EventFinish,
		Payload:   map[string]any{"status": "completed"},
		CreatedAt: now.Add(2 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Get("run_counts")
	if err != nil {
		t.Fatal(err)
	}
	if row.ToolCount != 1 {
		t.Fatalf("tool count = %d, want 1", row.ToolCount)
	}
	if row.Status != "completed" {
		t.Fatalf("status = %s, want completed", row.Status)
	}
}

func TestRunRecordRefreshToolCountFromToolCalls(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	now := time.Now().UTC()
	if err := repo.Start(model.RunRecord{
		ID:        "run_tools",
		SessionID: "session_1",
		Status:    "running",
		StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.Create(&model.ToolCall{
		ID:        "tool_1",
		RootRunID: "run_tools",
		SessionID: "session_1",
		ToolName:  "workspace.read_file",
		Status:    "completed",
		StartedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.RefreshToolCount("run_tools"); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Get("run_tools")
	if err != nil {
		t.Fatal(err)
	}
	if row.ToolCount != 1 {
		t.Fatalf("tool count = %d, want 1", row.ToolCount)
	}
}

func newRunRecordTestRepository(t *testing.T) RunRecordRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "runs.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.RunRecord{}, &model.ToolCall{}); err != nil {
		t.Fatal(err)
	}
	return NewRunRecordRepository(db)
}
