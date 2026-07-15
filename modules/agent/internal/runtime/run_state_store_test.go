package runtime

import (
	"context"
	"sync"
	"testing"

	"redpanda/protocol/methods"
)

func TestRunStateStoreCopiesSnapshots(t *testing.T) {
	var store RunStateStore
	todos := []methods.TodoItemDTO{{ID: "todo-1", Content: "original"}}
	goal := &runGoalState{Goal: methods.GoalDTO{ID: "goal-1", Status: "active"}, BoundToThisRun: true}
	goal.Goal.Criteria = []methods.GoalCriterionDTO{{ID: "criterion-1", Status: "unknown"}}

	store.SetTodos("run-1", todos)
	store.SetGoal("run-1", goal)
	todos[0].Content = "changed by caller"
	goal.Goal.Status = "failed"
	goal.Goal.Criteria[0].Status = "met"

	gotTodos := store.Todos("run-1")
	gotGoal := store.Goal("run-1")
	if gotTodos[0].Content != "original" {
		t.Fatalf("stored todos changed through input slice: %+v", gotTodos)
	}
	if gotGoal.Goal.Status != "active" {
		t.Fatalf("stored goal changed through input pointer: %+v", gotGoal)
	}
	if gotGoal.Goal.Criteria[0].Status != "unknown" {
		t.Fatalf("stored goal criteria changed through input slice: %+v", gotGoal.Goal.Criteria)
	}

	gotTodos[0].Content = "changed after read"
	gotGoal.Goal.Status = "cancelled"
	gotGoal.Goal.Criteria[0].Status = "blocked"
	if store.Todos("run-1")[0].Content != "original" {
		t.Fatal("stored todos changed through returned slice")
	}
	if store.Goal("run-1").Goal.Status != "active" {
		t.Fatal("stored goal changed through returned pointer")
	}
	if store.Goal("run-1").Goal.Criteria[0].Status != "unknown" {
		t.Fatal("stored goal criteria changed through returned slice")
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
	store.SetGoal("run-1", &runGoalState{Goal: methods.GoalDTO{ID: "goal-1"}})
	if got := store.NextRunSeq("run-1"); got != 1 {
		t.Fatalf("first run sequence = %d, want 1", got)
	}
	if got := store.NextWorkerSeq("run-1", "worker-1"); got != 1 {
		t.Fatalf("first worker sequence = %d, want 1", got)
	}

	store.Remove("run-1")
	if store.Cancel("run-1") != nil || store.Todos("run-1") != nil || store.Goal("run-1") != nil {
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
				store.SetGoal("run-concurrent", &runGoalState{Goal: methods.GoalDTO{ID: "goal"}})
				_ = store.Goal("run-concurrent")
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
