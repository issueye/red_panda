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

func TestRunEventRepositoryListAfter(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "events.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.RunEvent{}); err != nil {
		t.Fatal(err)
	}

	repo := NewRunEventRepository(db)
	for i := uint64(1); i <= 3; i++ {
		if err := repo.Save(events.EnvelopeV2{
			ProtocolVersion: events.ProtocolVersionV2,
			EventID:         "evt_" + string(rune('0'+i)),
			RunID:           "run_1",
			SessionID:       "session_1",
			AssignmentID:    "assignment_1",
			RunSeq:          i,
			WorkerSeq:       i,
			Worker:          events.EventWorkerRef{ID: "worker-01"},
			Type:            events.EventMessageDelta,
			Payload:         map[string]any{"delta": "x"},
			CreatedAt:       time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.ListAfter("run_1", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].RunSeq != 2 || items[1].RunSeq != 3 {
		t.Fatalf("run seqs = %d,%d; want 2,3", items[0].RunSeq, items[1].RunSeq)
	}
}

func TestRunEventRepositoryListsMessageStreamsBySession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session-events.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.RunRecord{}, &model.RunEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := NewRunEventRepository(db)
	for _, run := range []model.RunRecord{
		{ID: "run_1", SessionID: "session_1"},
		{ID: "run_2", SessionID: "session_2"},
	} {
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []events.EnvelopeV2{
		{EventID: "evt_reasoning", RunID: "run_1", SessionID: "session_1", RunSeq: 1, Type: events.EventReasoningDelta, Payload: map[string]any{"delta": "inspect"}},
		{EventID: "evt_tool", RunID: "run_1", SessionID: "session_1", RunSeq: 2, Type: events.EventToolStarted},
		{EventID: "evt_other", RunID: "run_2", SessionID: "session_2", RunSeq: 1, Type: events.EventMessageDelta, Payload: map[string]any{"delta": "other"}},
	} {
		if err := repo.Save(event); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.ListMessageStreamsBySession("session_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EventID != "evt_reasoning" {
		t.Fatalf("session stream events = %#v", items)
	}
}
