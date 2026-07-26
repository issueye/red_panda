package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"redpanda/protocol/methods"
)

func TestRunStateStoreCopiesSnapshots(t *testing.T) {
	var store RunStateStore
	todos := []methods.TodoItemDTO{{ID: "todo-1", Content: "original"}}

	store.SetTodos("run-1", todos)
	todos[0].Content = "changed by caller"

	gotTodos := store.Todos("run-1")
	if gotTodos[0].Content != "original" {
		t.Fatalf("stored todos changed through input slice: %+v", gotTodos)
	}

	gotTodos[0].Content = "changed after read"
	if store.Todos("run-1")[0].Content != "original" {
		t.Fatal("stored todos changed through returned slice")
	}
}

func TestRunStateStoreRemoveClearsLifecycleState(t *testing.T) {
	var store RunStateStore
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !store.Register("run-1", cancel) {
		t.Fatal("first registration was rejected")
	}
	if store.Register("run-1", cancel) {
		t.Fatal("duplicate registration was accepted")
	}
	store.SetTodos("run-1", []methods.TodoItemDTO{{ID: "todo-1"}})
	if got := store.NextRunSeq("run-1"); got != 1 {
		t.Fatalf("first run sequence = %d, want 1", got)
	}
	if got := store.NextWorkerSeq("run-1", "worker-1"); got != 1 {
		t.Fatalf("first worker sequence = %d, want 1", got)
	}

	store.Remove("run-1")
	if store.Cancel("run-1") != nil || store.Todos("run-1") != nil {
		t.Fatal("remove retained run lifecycle state")
	}
	store.mu.RLock()
	_, exists := store.runs["run-1"]
	store.mu.RUnlock()
	if exists {
		t.Fatal("remove retained the run entry")
	}
	if got := store.NextRunSeq("run-1"); got != 1 {
		t.Fatalf("run sequence after removal = %d, want 1", got)
	}
	if got := store.NextWorkerSeq("run-1", "worker-1"); got != 1 {
		t.Fatalf("worker sequence after removal = %d, want 1", got)
	}
	if !store.Register("run-1", cancel) {
		t.Fatal("registration after removal was rejected")
	}
}

func TestRunStateStoreRejectsDuplicateNilCancel(t *testing.T) {
	var store RunStateStore
	if !store.Register("run-1", nil) {
		t.Fatal("first registration was rejected")
	}
	if store.Register("run-1", nil) {
		t.Fatal("duplicate nil-cancel registration was accepted")
	}
}

func TestRunStateStorePauseBlocksUntilResume(t *testing.T) {
	var store RunStateStore
	if !store.Register("run-pause", nil) || !store.Pause("run-pause") {
		t.Fatal("run was not registered and paused")
	}
	done := make(chan error, 1)
	go func() { done <- store.WaitIfPaused(context.Background(), "run-pause") }()
	select {
	case err := <-done:
		t.Fatalf("paused waiter returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if !store.Resume("run-pause") {
		t.Fatal("run was not resumed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("paused waiter did not resume")
	}
}

func TestRunStateStorePausedWaitHonorsCancellation(t *testing.T) {
	var store RunStateStore
	if !store.Register("run-cancel", nil) || !store.Pause("run-cancel") {
		t.Fatal("run was not registered and paused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- store.WaitIfPaused(ctx, "run-cancel") }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled paused waiter did not return")
	}
}

func TestRunStateStoreConcurrentAccess(t *testing.T) {
	var store RunStateStore
	const workers = 16
	const increments = 200
	var wg sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < increments; i++ {
				store.NextRunSeq("run-concurrent")
				store.NextWorkerSeq("run-concurrent", "worker-shared")
				store.SetTodos("run-concurrent", []methods.TodoItemDTO{{ID: "todo"}})
				_ = store.Todos("run-concurrent")
			}
		}(worker)
	}
	wg.Wait()

	want := uint64(workers * increments)
	store.mu.RLock()
	state := store.runs["run-concurrent"]
	gotRunSeq := state.RunSeq
	gotWorkerSeq := state.WorkerSeq["worker-shared"]
	store.mu.RUnlock()
	if gotRunSeq != want || gotWorkerSeq != want {
		t.Fatalf("sequences = run:%d worker:%d, want %d", gotRunSeq, gotWorkerSeq, want)
	}
}
