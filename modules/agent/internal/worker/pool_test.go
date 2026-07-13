package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeExecutor struct {
	mu         sync.Mutex
	requests   []ExecuteRequest
	started    chan ExecuteRequest
	release    chan struct{}
	executeErr error
	closed     bool
}

type stubbornExecutor struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	closed  bool
}

type callbackExecutor struct {
	mu      sync.Mutex
	started chan struct{}
	proceed chan struct{}
}

func newCallbackExecutor() *callbackExecutor {
	return &callbackExecutor{started: make(chan struct{}), proceed: make(chan struct{})}
}
func (e *callbackExecutor) Execute(ctx context.Context, _ ExecuteRequest, sink EventSink) (ExecuteResult, error) {
	close(e.started)
	<-e.proceed
	e.mu.Lock()
	if sink != nil {
		sink(Event{Type: "cancel-self"})
	}
	e.mu.Unlock()
	<-ctx.Done()
	return ExecuteResult{}, ctx.Err()
}
func (e *callbackExecutor) Cancel(context.Context, AssignmentID, string) error {
	e.mu.Lock()
	e.mu.Unlock()
	return nil
}
func (e *callbackExecutor) Reset(context.Context) error { return nil }
func (e *callbackExecutor) Healthy() bool               { return true }
func (e *callbackExecutor) Close(context.Context) error { return nil }

func newStubbornExecutor() *stubbornExecutor {
	return &stubbornExecutor{started: make(chan struct{}), release: make(chan struct{})}
}

func (e *stubbornExecutor) Execute(context.Context, ExecuteRequest, EventSink) (ExecuteResult, error) {
	close(e.started)
	<-e.release
	return ExecuteResult{Output: "released"}, nil
}
func (e *stubbornExecutor) Cancel(context.Context, AssignmentID, string) error { return nil }
func (e *stubbornExecutor) Reset(context.Context) error                        { return nil }
func (e *stubbornExecutor) Healthy() bool                                      { return true }
func (e *stubbornExecutor) Close(context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	return nil
}

func newFakeExecutor(block bool) *fakeExecutor {
	f := &fakeExecutor{started: make(chan ExecuteRequest, 4)}
	if block {
		f.release = make(chan struct{})
	}
	return f
}

func (f *fakeExecutor) Execute(ctx context.Context, req ExecuteRequest, _ EventSink) (ExecuteResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	f.started <- req
	if f.release != nil {
		select {
		case <-ctx.Done():
			return ExecuteResult{}, ctx.Err()
		case <-f.release:
		}
	}
	if f.executeErr != nil {
		return ExecuteResult{}, f.executeErr
	}
	return ExecuteResult{Output: "done:" + req.Task}, nil
}

func (f *fakeExecutor) Cancel(context.Context, AssignmentID, string) error { return nil }
func (f *fakeExecutor) Reset(context.Context) error                        { return nil }
func (f *fakeExecutor) Healthy() bool                                      { return true }
func (f *fakeExecutor) Close(context.Context) error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func TestNewPoolEagerlyCreatesStableWorkersAndMailboxes(t *testing.T) {
	var ids []WorkerID
	pool, err := NewPool(Config{Size: 3, MailboxCapacity: 2}, func(id WorkerID) (Executor, error) {
		ids = append(ids, id)
		return newFakeExecutor(false), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close(context.Background()) })

	want := []WorkerID{"worker-01", "worker-02", "worker-03"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("factory worker IDs = %v, want %v", ids, want)
	}
	snapshot := pool.Snapshot()
	if snapshot.Configured != 3 || snapshot.Ready != 3 || len(snapshot.Workers) != 3 {
		t.Fatalf("unexpected initial snapshot: %+v", snapshot)
	}
	for i, worker := range snapshot.Workers {
		if worker.ID != want[i] || worker.State != WorkerReady || worker.MailboxCapacity != 2 {
			t.Fatalf("worker %d = %+v", i, worker)
		}
	}

	second := pool.Snapshot()
	for i := range snapshot.Workers {
		if snapshot.Workers[i].ID != second.Workers[i].ID {
			t.Fatalf("worker ID changed between snapshots: %q -> %q", snapshot.Workers[i].ID, second.Workers[i].ID)
		}
	}
}

func TestWorkerAndAssignmentStatesAreIndependent(t *testing.T) {
	executor := newFakeExecutor(false)
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })

	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "work"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pool.Wait(context.Background(), ref.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AssignmentCompleted || result.Output != "done:work" {
		t.Fatalf("unexpected result: %+v", result)
	}
	snapshot := pool.Snapshot()
	if snapshot.Workers[0].State != WorkerReady {
		t.Fatalf("worker retained assignment terminal state: %+v", snapshot.Workers[0])
	}
	assignment := findAssignment(t, snapshot, ref.AssignmentID)
	if assignment.Status != AssignmentCompleted || assignment.WorkerID != snapshot.Workers[0].ID {
		t.Fatalf("unexpected assignment snapshot: %+v", assignment)
	}
}

func TestExecutorFailureIsRecordedAsTerminalAssignment(t *testing.T) {
	executor := newFakeExecutor(false)
	executor.executeErr = errors.New("executor failed")
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })

	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "work"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pool.Wait(context.Background(), ref.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AssignmentFailed || result.Error != "executor failed" {
		t.Fatalf("unexpected failed result: %+v", result)
	}
	if pool.Cancel(context.Background(), ref.AssignmentID, "late cancel") {
		t.Fatal("late cancel changed a terminal assignment")
	}
	assignment := findAssignment(t, pool.Snapshot(), ref.AssignmentID)
	if assignment.Status != AssignmentFailed || assignment.Error != "executor failed" {
		t.Fatalf("terminal failure was overwritten: %+v", assignment)
	}
}

func TestBusyPoolReturnsCapacityErrorWithoutLedgerEntry(t *testing.T) {
	executor := newFakeExecutor(true)
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", ProfileKey: "planner", Task: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	busySnapshot := pool.Snapshot().Workers[0]
	if busySnapshot.CurrentAssignmentID != ref.AssignmentID || busySnapshot.ProfileKey != "planner" {
		t.Fatalf("busy worker projection = %+v", busySnapshot)
	}
	before := len(pool.Snapshot().Assignments)
	if _, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-2", Task: "overflow"}); !errors.Is(err, ErrCapacityExhausted) {
		t.Fatalf("busy submit error = %v, want ErrCapacityExhausted", err)
	}
	if after := len(pool.Snapshot().Assignments); after != before {
		t.Fatalf("rejected submit changed ledger: before=%d after=%d", before, after)
	}
	pool.Cancel(context.Background(), ref.AssignmentID, "cleanup")
}

func TestNewPoolRejectsInvalidResourceLimits(t *testing.T) {
	tests := []Config{
		{Size: -1},
		{Size: MaxPoolSize + 1},
		{Size: 1, MailboxCapacity: -1},
		{Size: 1, MaxMessageBytes: -1},
		{Size: 1, MessageTTL: -1},
	}
	for _, config := range tests {
		if _, err := NewPool(config, func(WorkerID) (Executor, error) { return newFakeExecutor(false), nil }); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("NewPool(%+v) error = %v, want ErrInvalidConfig", config, err)
		}
	}
}

func TestMailboxIsBoundedAndMessagesAreDirected(t *testing.T) {
	executors := map[WorkerID]*fakeExecutor{}
	pool := mustPool(t, Config{Size: 2, MailboxCapacity: 1}, func(id WorkerID) (Executor, error) {
		executor := newFakeExecutor(true)
		executors[id] = executor
		return executor, nil
	})
	entryRef, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[entryRef.WorkerID].started
	delegatedRef, err := pool.Delegate(context.Background(), entryRef.AssignmentID, SubmitRequest{RunID: "run-1", Task: "delegate"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[delegatedRef.WorkerID].started
	message := WorkerMessage{
		RunID:        "run-1",
		FromWorkerID: entryRef.WorkerID,
		ToWorkerID:   delegatedRef.WorkerID,
		Kind:         MessageUpdate,
		Payload:      json.RawMessage(`{"progress":1}`),
	}
	if err := pool.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err := pool.Send(context.Background(), message); !errors.Is(err, ErrMailboxFull) {
		t.Fatalf("second send error = %v, want ErrMailboxFull", err)
	}
	received, err := pool.Receive(context.Background(), delegatedRef.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if received.FromWorkerID != entryRef.WorkerID || received.ToWorkerID != delegatedRef.WorkerID || received.ID == "" {
		t.Fatalf("unexpected delivered message: %+v", received)
	}

	message.RunID = "other-run"
	if err := pool.Send(context.Background(), message); !errors.Is(err, ErrCrossRunMessage) {
		t.Fatalf("cross-run send error = %v, want ErrCrossRunMessage", err)
	}
}

func TestCancelMakesAssignmentTerminal(t *testing.T) {
	executor := newFakeExecutor(true)
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	if !pool.Cancel(context.Background(), ref.AssignmentID, "test") {
		t.Fatal("Cancel returned false")
	}
	result, err := pool.Wait(context.Background(), ref.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AssignmentCancelled || result.Error != "test" {
		t.Fatalf("unexpected cancelled result: %+v", result)
	}
	time.Sleep(10 * time.Millisecond)
	assignment := findAssignment(t, pool.Snapshot(), ref.AssignmentID)
	if assignment.Status != AssignmentCancelled {
		t.Fatalf("late executor completion overwrote terminal state: %+v", assignment)
	}
}

func TestDelegatedAssignmentCannotDelegateEvenWithSpoofedOrigin(t *testing.T) {
	executors := map[WorkerID]*fakeExecutor{}
	pool := mustPool(t, Config{Size: 2}, func(id WorkerID) (Executor, error) {
		executor := newFakeExecutor(true)
		executors[id] = executor
		return executor, nil
	})
	entry, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[entry.WorkerID].started
	delegated, err := pool.Delegate(context.Background(), entry.AssignmentID, SubmitRequest{RunID: "run-1", Task: "child"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[delegated.WorkerID].started

	before := len(pool.Snapshot().Assignments)
	_, err = pool.Delegate(context.Background(), delegated.AssignmentID, SubmitRequest{RunID: "run-1", Task: "nested"})
	if !errors.Is(err, ErrNestedDelegation) {
		t.Fatalf("nested delegation error = %v, want ErrNestedDelegation", err)
	}
	after := len(pool.Snapshot().Assignments)
	if after != before {
		t.Fatalf("nested delegation created ledger record: before=%d after=%d", before, after)
	}
}

func TestRestrictedRemoteAssignmentCannotDelegate(t *testing.T) {
	executors := map[WorkerID]*fakeExecutor{}
	pool := mustPool(t, Config{Size: 2}, func(id WorkerID) (Executor, error) {
		executor := newFakeExecutor(true)
		executors[id] = executor
		return executor, nil
	})
	restricted, err := pool.SubmitRestricted(context.Background(), "remote-worker-02", SubmitRequest{RunID: "run-remote", Task: "child runtime entry"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[restricted.WorkerID].started
	if restricted.OriginWorkerID != "remote-worker-02" {
		t.Fatalf("restricted origin = %q", restricted.OriginWorkerID)
	}
	if _, err := pool.Delegate(context.Background(), restricted.AssignmentID, SubmitRequest{RunID: "run-remote", Task: "nested"}); !errors.Is(err, ErrNestedDelegation) {
		t.Fatalf("restricted Delegate error = %v, want %v", err, ErrNestedDelegation)
	}
}

func TestEntryAssignmentCanDelegateButCallerMustBeTrusted(t *testing.T) {
	executors := map[WorkerID]*fakeExecutor{}
	pool := mustPool(t, Config{Size: 2}, func(id WorkerID) (Executor, error) {
		executor := newFakeExecutor(true)
		executors[id] = executor
		return executor, nil
	})
	entry, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	<-executors[entry.WorkerID].started
	delegated, err := pool.Delegate(context.Background(), entry.AssignmentID, SubmitRequest{RunID: "run-1", Task: "child"})
	if err != nil {
		t.Fatal(err)
	}
	if delegated.OriginWorkerID != entry.WorkerID {
		t.Fatalf("delegated origin = %q, want trusted caller worker %q", delegated.OriginWorkerID, entry.WorkerID)
	}
	if _, err := pool.Delegate(context.Background(), "missing", SubmitRequest{RunID: "run-1", Task: "bad"}); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("unknown caller error = %v, want ErrAssignmentNotFound", err)
	}
	if _, err := pool.Delegate(context.Background(), entry.AssignmentID, SubmitRequest{RunID: "other-run", Task: "bad"}); !errors.Is(err, ErrCallerRunMismatch) {
		t.Fatalf("caller run mismatch error = %v, want ErrCallerRunMismatch", err)
	}
}

func TestCloseCancelsAssignmentsClosesResourcesAndRejectsWork(t *testing.T) {
	executor := newFakeExecutor(true)
	pool, err := NewPool(Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	if err != nil {
		t.Fatal(err)
	}
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	if err := pool.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := pool.Wait(context.Background(), ref.AssignmentID)
	if err != nil || result.Status != AssignmentCancelled {
		t.Fatalf("closed assignment result=%+v err=%v", result, err)
	}
	if _, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-2", Task: "no"}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("submit after close error = %v, want ErrPoolClosed", err)
	}
	executor.mu.Lock()
	closed := executor.closed
	executor.mu.Unlock()
	if !closed || pool.Snapshot().Workers[0].State != WorkerStopped {
		t.Fatalf("resources not closed: executor=%v snapshot=%+v", closed, pool.Snapshot())
	}
}

func TestMessagesDoNotLeakAcrossWorkerReuseAndExpiredMessagesAreDiscarded(t *testing.T) {
	executors := map[WorkerID]*fakeExecutor{}
	pool := mustPool(t, Config{Size: 2, MailboxCapacity: 4}, func(id WorkerID) (Executor, error) {
		executor := newFakeExecutor(true)
		executors[id] = executor
		return executor, nil
	})
	entry1, _ := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "entry"})
	<-executors[entry1.WorkerID].started
	child1, _ := pool.Delegate(context.Background(), entry1.AssignmentID, SubmitRequest{RunID: "run-1", Task: "child"})
	<-executors[child1.WorkerID].started
	old := WorkerMessage{RunID: "run-1", FromWorkerID: entry1.WorkerID, ToWorkerID: child1.WorkerID, Kind: MessageUpdate, Payload: json.RawMessage(`{"old":true}`)}
	if err := pool.Send(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if got := pool.CancelRun(context.Background(), "run-1", "next run"); got != 2 {
		t.Fatalf("CancelRun = %d, want 2", got)
	}
	waitReady(t, pool, 2)

	entry2, _ := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-2", Task: "entry"})
	<-executors[entry2.WorkerID].started
	child2, _ := pool.Delegate(context.Background(), entry2.AssignmentID, SubmitRequest{RunID: "run-2", Task: "child"})
	<-executors[child2.WorkerID].started
	old.RunID = "run-1"
	if err := pool.Send(context.Background(), old); !errors.Is(err, ErrCrossRunMessage) {
		t.Fatalf("old-run send = %v", err)
	}
	expiring := WorkerMessage{RunID: "run-2", FromWorkerID: entry2.WorkerID, ToWorkerID: child2.WorkerID, Kind: MessageUpdate, ExpiresAt: time.Now().Add(5 * time.Millisecond), Payload: json.RawMessage(`{"expired":true}`)}
	if err := pool.Send(context.Background(), expiring); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	fresh := WorkerMessage{RunID: "run-2", FromWorkerID: entry2.WorkerID, ToWorkerID: child2.WorkerID, Kind: MessageResult, Payload: json.RawMessage(`{"fresh":true}`)}
	if err := pool.Send(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	received, err := pool.Receive(context.Background(), child2.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if received.RunID != "run-2" || received.Kind != MessageResult {
		t.Fatalf("received stale message: %+v", received)
	}
}

func TestCloseTimeoutDoesNotCloseRunningExecutorAndLaterCallWaits(t *testing.T) {
	executor := newStubbornExecutor()
	pool, err := NewPool(Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "stubborn"}); err != nil {
		t.Fatal(err)
	}
	<-executor.started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := pool.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v", err)
	}
	executor.mu.Lock()
	closedEarly := executor.closed
	executor.mu.Unlock()
	if closedEarly {
		t.Fatal("executor closed while Execute was still running")
	}
	done := make(chan error, 1)
	go func() { done <- pool.Close(context.Background()) }()
	select {
	case <-done:
		t.Fatal("concurrent Close returned before shutdown completed")
	case <-time.After(10 * time.Millisecond):
	}
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCancelledAssignmentIsNotSettledUntilExecuteReleasesWorker(t *testing.T) {
	executor := newStubbornExecutor()
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-settled", Task: "stubborn"})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	if !pool.Cancel(context.Background(), ref.AssignmentID, "stop") {
		t.Fatal("Cancel returned false")
	}
	if _, err := pool.Wait(context.Background(), ref.AssignmentID); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := pool.WaitSettled(short, ref.AssignmentID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitSettled before release = %v", err)
	}
	close(executor.release)
	if _, err := pool.WaitSettled(context.Background(), ref.AssignmentID); err != nil {
		t.Fatal(err)
	}
	if snapshot := pool.Snapshot(); snapshot.Ready != 1 || snapshot.Workers[0].CurrentAssignmentID != "" {
		t.Fatalf("worker not released after settle: %+v", snapshot)
	}
}

func TestTerminalAssignmentCanBeRemoved(t *testing.T) {
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return newFakeExecutor(false), nil })
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Wait(context.Background(), ref.AssignmentID); err != nil {
		t.Fatal(err)
	}
	if !pool.RemoveTerminal(ref.AssignmentID) {
		t.Fatal("RemoveTerminal returned false")
	}
	if _, err := pool.Wait(context.Background(), ref.AssignmentID); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("Wait after removal = %v", err)
	}
}

func TestEventSinkCanCancelItsOwnAssignmentWithoutExecutorLockDeadlock(t *testing.T) {
	executor := newCallbackExecutor()
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	var ref AssignmentRef
	var err error
	ref, err = pool.SubmitEntry(context.Background(), SubmitRequest{
		RunID: "run-1", Task: "callback",
		EventSink: func(Event) { pool.Cancel(context.Background(), ref.AssignmentID, "from sink") },
	})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	close(executor.proceed)
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := pool.Wait(waitCtx, ref.AssignmentID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != AssignmentCancelled {
		t.Fatalf("result = %+v", result)
	}
}

func TestWaitingPermissionStateCanBeProjectedAndRestored(t *testing.T) {
	executor := newFakeExecutor(true)
	pool := mustPool(t, Config{Size: 1}, func(WorkerID) (Executor, error) { return executor, nil })
	ref, err := pool.SubmitEntry(context.Background(), SubmitRequest{RunID: "run-1", Task: "permission"})
	if err != nil {
		t.Fatal(err)
	}
	<-executor.started
	if err := pool.SetWaitingPermission(ref.AssignmentID, true); err != nil {
		t.Fatal(err)
	}
	if got := pool.Snapshot().WaitingPermission; got != 1 {
		t.Fatalf("WaitingPermission = %d", got)
	}
	if err := pool.SetWaitingPermission(ref.AssignmentID, false); err != nil {
		t.Fatal(err)
	}
	if got := pool.Snapshot().Running; got != 1 {
		t.Fatalf("Running = %d", got)
	}
}

func mustPool(t *testing.T, config Config, factory ExecutorFactory) *Pool {
	t.Helper()
	pool, err := NewPool(config, factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	return pool
}

func submitAndWait(t *testing.T, pool *Pool, request SubmitRequest) AssignmentSnapshot {
	t.Helper()
	ref, err := pool.SubmitEntry(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Wait(context.Background(), ref.AssignmentID); err != nil {
		t.Fatal(err)
	}
	return findAssignment(t, pool.Snapshot(), ref.AssignmentID)
}

func findAssignment(t *testing.T, snapshot PoolSnapshot, id AssignmentID) AssignmentSnapshot {
	t.Helper()
	for _, assignment := range snapshot.Assignments {
		if assignment.ID == id {
			return assignment
		}
	}
	t.Fatalf("assignment %q not found", id)
	return AssignmentSnapshot{}
}

func waitReady(t *testing.T, pool *Pool, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pool.Snapshot().Ready == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pool did not reach %d ready workers: %+v", count, pool.Snapshot())
}
