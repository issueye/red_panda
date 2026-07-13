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
