package database

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"redpanda/gateway/internal/gateway/model"
)

func TestOpenSerializesConcurrentWrites(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout;").Scan(&busyTimeout).Error; err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	const workers = 12
	const perWorker = 20
	var wg sync.WaitGroup
	errCh := make(chan error, workers*perWorker)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("session_%d_%d", worker, i)
				if err := db.Create(&model.Session{ID: id, Name: id, Status: "active"}).Error; err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	var count int64
	if err := db.Model(&model.Session{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != workers*perWorker {
		t.Fatalf("session count = %d, want %d", count, workers*perWorker)
	}
}
