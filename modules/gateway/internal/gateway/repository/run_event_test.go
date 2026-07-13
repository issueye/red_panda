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
		if err := repo.Save(events.Envelope{
			ProtocolVersion: events.ProtocolVersion,
			EventID:         "evt_" + string(rune('0'+i)),
			RootRunID:       "run_1",
			RunID:           "run_1",
			SessionID:       "session_1",
			RootSeq:         i,
			AgentSeq:        i,
			Agent: events.AgentRef{
				AgentID: "root",
				Role:    events.AgentRoleRoot,
				Path:    []string{"root"},
			},
			Type:      events.EventMessageDelta,
			Payload:   map[string]any{"delta": "x"},
			CreatedAt: time.Now().UTC(),
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
	if items[0].RootSeq != 2 || items[1].RootSeq != 3 {
		t.Fatalf("root seqs = %d,%d; want 2,3", items[0].RootSeq, items[1].RootSeq)
	}
}

func TestRunEventRepositorySaveOnceDeduplicatesEventID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "events-dedupe.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.RunEvent{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	repo := NewRunEventRepository(db)
	event := events.Envelope{
		ProtocolVersion: events.ProtocolVersion,
		EventID:         "evt_once", RootRunID: "run_once", RunID: "run_once", RootSeq: 1,
		Type: events.EventFinish, Payload: map[string]any{"status": "completed"}, CreatedAt: time.Now().UTC(),
	}
	inserted, err := repo.SaveOnce(event)
	if err != nil || !inserted {
		t.Fatalf("first SaveOnce = inserted %v, err %v", inserted, err)
	}
	inserted, err = repo.SaveOnce(event)
	if err != nil || inserted {
		t.Fatalf("duplicate SaveOnce = inserted %v, err %v", inserted, err)
	}
	items, err := repo.ListAfter("run_once", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("persisted duplicate events: %d", len(items))
	}
}
