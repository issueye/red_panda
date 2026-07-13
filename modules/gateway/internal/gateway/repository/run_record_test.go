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
	if err := repo.ProjectEvent(events.EnvelopeV2{
		EventID:   "evt_run_1_1",
		RunID:     "run_1",
		SessionID: "session_1",
		RunSeq:    1,
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
	if err := repo.ProjectEvent(events.EnvelopeV2{
		EventID:   "evt_run_1_2",
		RunID:     "run_1",
		SessionID: "session_1",
		RunSeq:    2,
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
	if err := repo.ProjectEvent(events.EnvelopeV2{
		EventID:   "evt_tool",
		RunID:     "run_counts",
		SessionID: "session_1",
		RunSeq:    1,
		Type:      events.EventToolStarted,
		CreatedAt: now.Add(time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProjectEvent(events.EnvelopeV2{
		EventID:   "evt_finish",
		RunID:     "run_counts",
		SessionID: "session_1",
		RunSeq:    2,
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

func TestRunRecordProjectionIgnoresWorkerAssignmentFailureForRunLifecycle(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	now := time.Now().UTC()
	if err := repo.Start(model.RunRecord{
		ID:        "run_subagent_error",
		SessionID: "session_1",
		Status:    "running",
		StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProjectEvent(events.EnvelopeV2{
		EventID:      "evt_worker_failed",
		RunID:        "run_subagent_error",
		SessionID:    "session_1",
		AssignmentID: "assignment_1",
		RunSeq:       1,
		Type:         events.EventWorkerAssignmentUpdated,
		Worker:       events.EventWorkerRef{ID: "worker-01"},
		Payload:      map[string]any{"status": "failed", "message": "provider timeout"},
		CreatedAt:    now.Add(time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Get("run_subagent_error")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "running" || row.FinishedAt != nil || row.Error != "" {
		t.Fatalf("worker assignment failure changed run lifecycle: %#v", row)
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
		RunID:     "run_tools",
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

func TestRunRecordFinishReleasesActiveSlot(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	now := time.Now().UTC()
	if err := repo.Start(model.RunRecord{
		ID:        "run_finish",
		SessionID: "session_finish",
		Status:    "running",
		StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	active, err := repo.CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("CountActive before finish = %d, want 1", active)
	}
	if err := repo.Finish("run_finish", "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	active, err = repo.CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("CountActive after finish = %d, want 0", active)
	}
	row, err := repo.Get("run_finish")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "failed" || row.Error != "boom" || row.FinishedAt == nil {
		t.Fatalf("finish projection mismatch: %#v", row)
	}
}

func TestRunRecordCountActiveAndListActive(t *testing.T) {
	repo := newRunRecordTestRepository(t)
	now := time.Now().UTC()
	records := []model.RunRecord{
		{ID: "run_a", SessionID: "session_1", Status: "running", StartedAt: now},
		{ID: "run_b", SessionID: "session_1", Status: "waiting_permission", StartedAt: now.Add(time.Millisecond)},
		{ID: "run_c", SessionID: "session_2", Status: "running", StartedAt: now.Add(2 * time.Millisecond)},
		{ID: "run_d", SessionID: "session_2", Status: "completed", StartedAt: now.Add(3 * time.Millisecond)},
		{ID: "run_e", SessionID: "session_3", Status: "failed", StartedAt: now.Add(4 * time.Millisecond)},
	}
	for _, record := range records {
		if err := repo.Start(record); err != nil {
			t.Fatal(err)
		}
	}

	active, err := repo.CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if active != 3 {
		t.Fatalf("CountActive = %d, want 3", active)
	}

	session1, err := repo.CountActiveBySession("session_1")
	if err != nil {
		t.Fatal(err)
	}
	if session1 != 2 {
		t.Fatalf("CountActiveBySession(session_1) = %d, want 2", session1)
	}

	session2, err := repo.CountActiveBySession("session_2")
	if err != nil {
		t.Fatal(err)
	}
	if session2 != 1 {
		t.Fatalf("CountActiveBySession(session_2) = %d, want 1", session2)
	}

	listed, err := repo.ListActive(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListActive len = %d, want 3", len(listed))
	}
	if listed[0].ID != "run_a" || listed[1].ID != "run_b" || listed[2].ID != "run_c" {
		t.Fatalf("ListActive order = %#v, want run_a, run_b, run_c", listed)
	}

	bySession, err := repo.ListActiveBySession("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(bySession) != 2 || bySession[0].ID != "run_a" || bySession[1].ID != "run_b" {
		t.Fatalf("ListActiveBySession(session_1) = %#v, want run_a, run_b", bySession)
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
