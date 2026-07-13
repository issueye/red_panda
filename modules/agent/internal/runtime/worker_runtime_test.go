package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	protocoltools "redpanda/protocol/tools"
)

func TestNewCreatesConfiguredWorkerPoolWithoutStartingProcess(t *testing.T) {
	t.Setenv("RED_PANDA_WORKER_POOL_SIZE", "99")
	// If New eagerly starts a child process this invalid command makes the test fail.
	t.Setenv("RED_PANDA_SUBAGENT_COMMAND", "definitely-not-a-red-panda-binary")
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	snapshot := rt.workerPool.Snapshot()
	if snapshot.Configured != worker.MaxPoolSize || snapshot.Ready != worker.MaxPoolSize {
		t.Fatalf("WorkerPool snapshot = %#v, want %d ready Workers", snapshot, worker.MaxPoolSize)
	}
}

func TestHandleReplyReturnsEntryAssignmentIdentity(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 16)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	sendRequest(t, context.Background(), rt, "reply-worker-entry", methods.AgentReply, methods.ReplyParams{
		RunID:   "run-worker-entry",
		Session: methods.ReplySession{ID: "session-worker-entry"},
		Input:   methods.ReplyInput{Text: "hello"},
	})
	response := waitForResponse(t, lines, jsonrpc.ID("reply-worker-entry"))
	var accepted methods.RunExecuteResult
	if err := json.Unmarshal(response.Result, &accepted); err != nil {
		t.Fatal(err)
	}
	if !accepted.Accepted || accepted.AssignmentID == "" || accepted.WorkerID == "" {
		t.Fatalf("accepted response = %#v", accepted)
	}

	found := false
	for _, assignment := range rt.workerPool.Snapshot().Assignments {
		if string(assignment.ID) == accepted.AssignmentID && string(assignment.WorkerID) == accepted.WorkerID && assignment.RunID == accepted.RunID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("accepted assignment is not in WorkerPool ledger: %#v", rt.workerPool.Snapshot())
	}
}

func TestRunExecuteEmitsV2WorkerEnvelope(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "0.2.0-test")
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	ctx := context.Background()
	sendRequest(t, ctx, rt, "init-v2", methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersionV2,
		Client:          methods.PeerInfo{Name: "test-gateway", Version: "0.2.0"},
	})
	initResponse := waitForResponse(t, lines, jsonrpc.ID("init-v2"))
	var initialized methods.InitializeResult
	if err := json.Unmarshal(initResponse.Result, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != events.ProtocolVersionV2 {
		t.Fatalf("protocol = %q", initialized.ProtocolVersion)
	}

	sendRequest(t, ctx, rt, "run-v2", methods.RunExecute, methods.RunExecuteParams{
		RunID:   "run-v2",
		Session: methods.ReplySession{ID: "session-v2"},
		Input:   methods.ReplyInput{Text: "hello"},
	})
	response := waitForResponse(t, lines, jsonrpc.ID("run-v2"))
	var accepted methods.RunExecuteResult
	if err := json.Unmarshal(response.Result, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.AssignmentID == "" || accepted.WorkerID == "" {
		t.Fatalf("accepted = %#v", accepted)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case raw := <-lines:
			var note jsonrpc.Notification
			if err := json.Unmarshal(raw, &note); err != nil || note.Method != methods.RunEvent {
				continue
			}
			var event events.EnvelopeV2
			if err := json.Unmarshal(note.Params, &event); err != nil {
				t.Fatal(err)
			}
			if event.RunID != "run-v2" || event.SessionID != "session-v2" || event.AssignmentID != accepted.AssignmentID || event.Worker.ID != accepted.WorkerID {
				t.Fatalf("v2 event identity mismatch: %#v", event)
			}
			if event.Type == events.EventFinish {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for v2 finish")
		}
	}
}

func TestDelegatedWorkerReplyHidesDelegateAndLegacyRunTools(t *testing.T) {
	parent := methods.ReplyParams{RunID: "run-delegated", Options: methods.ReplyOptions{
		WorkerProfiles: []methods.WorkerProfileRef{{
			Key: "reviewer", Enabled: true, SystemPrompt: "Worker profile prompt.", DefaultMaxTurns: 9,
			ProviderName: "openai_compatible", Model: "worker-model", ToolPolicy: "strict",
			ToolAllowlist: []string{"workspace.read_file"}, ToolDenylist: []string{"shell.exec"},
		}},
		WorkerProfiles: []methods.WorkerProfileRef{{
			Key: "reviewer", Enabled: true, SystemPrompt: "Review with evidence.", DefaultMaxTurns: 7,
		}},
	}}
	child := delegatedWorkerReply(parent, "inspect runtime", "reviewer", 0)
	definitions := agenttools.AvailableToolsForOptions((agenttools.ToolRunner{}).AvailableTools(), child.Options)
	communication := map[string]bool{}
	for _, definition := range definitions {
		if definition.Name == "worker.delegate" || definition.Name == "worker.delegate" {
			t.Fatalf("delegated Worker exposes forbidden tool %q", definition.Name)
		}
		communication[definition.Name] = true
	}
	for _, denied := range []string{"worker.delegate", "worker.delegate"} {
		if !agenttools.ContainsString(child.Options.ToolDenylist, denied) {
			t.Fatalf("delegated denylist missing %q: %#v", denied, child.Options.ToolDenylist)
		}
	}
	if !communication["worker.send"] || !communication["worker.receive"] {
		t.Fatalf("delegated Worker cannot communicate: %#v", communication)
	}
	if child.Options.MaxToolTurns != 9 || child.Options.ProviderName != "openai_compatible" || child.Options.Model != "worker-model" || child.Options.ToolPolicy != "strict" {
		t.Fatalf("worker profile policy not applied: %#v", child.Options)
	}
	if child.Options.SpecialistContext == nil || !strings.Contains(child.Options.SpecialistContext.Context, "Worker profile prompt.") {
		t.Fatalf("worker profile prompt not applied: %#v", child.Options.SpecialistContext)
	}
}

func TestForgedNestedWorkerDelegateIsRejectedByPool(t *testing.T) {
	executor := &blockingRuntimeWorkerExecutor{started: make(chan worker.ExecuteRequest, 3)}
	pool, err := worker.NewPool(worker.Config{Size: 3}, func(worker.WorkerID) (worker.Executor, error) {
		return executor, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{workerPool: pool}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = pool.Close(closeCtx)
	}()

	entry, err := pool.SubmitEntry(ctx, worker.SubmitRequest{RunID: "run-nested", Task: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, entry.AssignmentID)
	delegated, err := pool.Delegate(ctx, entry.AssignmentID, worker.SubmitRequest{RunID: "run-nested", Task: "delegated"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, delegated.AssignmentID)

	reply := methods.ReplyParams{RunID: "run-nested"}
	_, err = rt.executeWorkerDelegate(ctx, agenttools.ToolRunContext{
		RunID:        "run-nested",
		AssignmentID: string(delegated.AssignmentID),
		Reply:        &reply,
	}, protocoltools.Call{Name: "worker.delegate", Arguments: map[string]any{"task": "forged nested work"}})
	if !errors.Is(err, worker.ErrNestedDelegation) {
		t.Fatalf("forged nested delegate error = %v, want %v", err, worker.ErrNestedDelegation)
	}
}

func TestParallelAssignmentsExchangeMessagesAndRejectForgedContext(t *testing.T) {
	executor := &blockingRuntimeWorkerExecutor{started: make(chan worker.ExecuteRequest, 3)}
	pool, err := worker.NewPool(worker.Config{Size: 3}, func(worker.WorkerID) (worker.Executor, error) {
		return executor, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &Runtime{workerPool: pool}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = pool.Close(closeCtx)
	}()

	entry, err := pool.SubmitEntry(ctx, worker.SubmitRequest{RunID: "run-chat", Task: "entry"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, entry.AssignmentID)
	delegated, err := pool.Delegate(ctx, entry.AssignmentID, worker.SubmitRequest{RunID: "run-chat", Task: "delegated"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, delegated.AssignmentID)

	entryCtx := agenttools.ToolRunContext{RunID: "run-chat", AssignmentID: string(entry.AssignmentID), WorkerID: string(entry.WorkerID)}
	bridge := &lazyProcessExecutor{
		runtime: rt, workerID: delegated.WorkerID, activeAssignmentID: delegated.AssignmentID,
		activeRunID: "run-chat", activeKind: workerExecutionDelegated,
	}
	bridgeToken := &methods.WorkerExecutionContext{
		RunID: "run-chat", WorkerID: string(delegated.WorkerID),
		AssignmentID: string(delegated.AssignmentID), ProxyMessages: true,
	}
	_, err = rt.executeWorkerSend(ctx, entryCtx, protocoltools.Call{ID: "entry-to-delegated", Arguments: map[string]any{
		"to_worker_id": string(delegated.WorkerID), "to_assignment_id": string(delegated.AssignmentID),
		"kind": "request", "payload": map[string]any{"question": "status?"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	receivedRaw, err := bridge.handleChildRequest(ctx, internalWorkerReceiveMethod, map[string]any{"worker_context": bridgeToken})
	if err != nil {
		t.Fatal(err)
	}
	var received worker.WorkerMessage
	if err := json.Unmarshal(receivedRaw, &received); err != nil {
		t.Fatal(err)
	}
	if received.FromAssignmentID != entry.AssignmentID || received.ToAssignmentID != delegated.AssignmentID {
		t.Fatalf("entry -> delegated message identities = %#v", received)
	}

	_, err = bridge.handleChildRequest(ctx, internalWorkerSendMethod, map[string]any{
		"to_worker_id": string(entry.WorkerID), "to_assignment_id": string(entry.AssignmentID),
		"kind": "result", "payload": map[string]any{"answer": "ready"}, "worker_context": bridgeToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	receivedJSON, err := rt.executeWorkerReceive(ctx, entryCtx, protocoltools.Call{})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(receivedJSON), &received); err != nil {
		t.Fatal(err)
	}
	if received.FromAssignmentID != delegated.AssignmentID || received.ToAssignmentID != entry.AssignmentID {
		t.Fatalf("delegated -> entry message identities = %#v", received)
	}

	other, err := pool.SubmitEntry(ctx, worker.SubmitRequest{RunID: "run-other", Task: "other"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, other.AssignmentID)
	_, err = rt.executeWorkerSend(ctx, entryCtx, protocoltools.Call{ID: "cross-run", Arguments: map[string]any{
		"to_worker_id": string(other.WorkerID), "kind": "update", "payload": "forbidden",
	}})
	if !errors.Is(err, worker.ErrCrossRunMessage) {
		t.Fatalf("cross-run send error = %v, want %v", err, worker.ErrCrossRunMessage)
	}

	forged := entryCtx
	forged.WorkerID = string(delegated.WorkerID)
	_, err = rt.executeWorkerSend(ctx, forged, protocoltools.Call{ID: "forged", Arguments: map[string]any{
		"to_worker_id": string(delegated.WorkerID), "kind": "control", "payload": "forbidden",
	}})
	if !errors.Is(err, worker.ErrAssignmentNotActive) {
		t.Fatalf("forged Worker context error = %v, want %v", err, worker.ErrAssignmentNotActive)
	}

	oldToken := *bridgeToken
	oldToken.AssignmentID = "assignment-old"
	_, err = bridge.handleChildRequest(ctx, internalWorkerSendMethod, map[string]any{
		"to_worker_id": string(entry.WorkerID), "kind": "update", "payload": "stale",
		"worker_context": &oldToken,
	})
	if !errors.Is(err, worker.ErrAssignmentNotActive) {
		t.Fatalf("stale bridge token error = %v, want %v", err, worker.ErrAssignmentNotActive)
	}
}

func TestWorkerListRedactsOtherParallelRun(t *testing.T) {
	rt, pool, executor, first, second := parallelRunWorkerRuntime(t)
	defer closeWorkerPoolForTest(t, pool)

	raw, err := rt.executeWorkerList(context.Background(), agenttools.ToolRunContext{RunID: first.RunID}, protocoltools.Call{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "other-run-secret") {
		t.Fatalf("worker.list leaked another run task: %s", raw)
	}
	var result struct {
		Workers     []worker.WorkerSnapshot     `json:"workers"`
		Assignments []worker.AssignmentSnapshot `json:"assignments"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 1 || result.Assignments[0].ID != first.AssignmentID {
		t.Fatalf("visible assignments = %+v", result.Assignments)
	}
	assertOtherRunWorkerRedacted(t, result.Workers, second)
	pool.CancelRun(context.Background(), first.RunID, "cleanup")
	pool.CancelRun(context.Background(), second.RunID, "cleanup")
	_ = executor
}

func TestWorkerPoolStatusRedactsOtherParallelRun(t *testing.T) {
	rt, pool, _, first, second := parallelRunWorkerRuntime(t)
	defer closeWorkerPoolForTest(t, pool)

	raw, err := rt.executeWorkerPoolStatus(context.Background(), agenttools.ToolRunContext{RunID: first.RunID}, protocoltools.Call{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "other-run-secret") {
		t.Fatalf("worker.pool_status leaked another run task: %s", raw)
	}
	var result struct {
		Pool worker.PoolSnapshot `json:"pool"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pool.Busy != 2 || result.Pool.Running != 2 {
		t.Fatalf("global capacity counters were not preserved: %+v", result.Pool)
	}
	if len(result.Pool.Assignments) != 1 || result.Pool.Assignments[0].ID != first.AssignmentID {
		t.Fatalf("visible assignments = %+v", result.Pool.Assignments)
	}
	assertOtherRunWorkerRedacted(t, result.Pool.Workers, second)
	pool.CancelRun(context.Background(), first.RunID, "cleanup")
	pool.CancelRun(context.Background(), second.RunID, "cleanup")
}

type parallelRunRef struct {
	RunID        string
	AssignmentID worker.AssignmentID
	WorkerID     worker.WorkerID
}

func parallelRunWorkerRuntime(t *testing.T) (*Runtime, *worker.Pool, *blockingRuntimeWorkerExecutor, parallelRunRef, parallelRunRef) {
	t.Helper()
	executor := &blockingRuntimeWorkerExecutor{started: make(chan worker.ExecuteRequest, 2)}
	pool, err := worker.NewPool(worker.Config{Size: 2}, func(worker.WorkerID) (worker.Executor, error) { return executor, nil })
	if err != nil {
		t.Fatal(err)
	}
	firstRef, err := pool.SubmitEntry(context.Background(), worker.SubmitRequest{RunID: "run-visible", ProfileKey: "visible-profile", Task: "visible-run-task"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, firstRef.AssignmentID)
	secondRef, err := pool.SubmitEntry(context.Background(), worker.SubmitRequest{RunID: "run-other", ProfileKey: "secret-profile", Task: "other-run-secret"})
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimeWorkerStart(t, executor.started, secondRef.AssignmentID)
	return &Runtime{workerPool: pool}, pool, executor,
		parallelRunRef{RunID: "run-visible", AssignmentID: firstRef.AssignmentID, WorkerID: firstRef.WorkerID},
		parallelRunRef{RunID: "run-other", AssignmentID: secondRef.AssignmentID, WorkerID: secondRef.WorkerID}
}

func assertOtherRunWorkerRedacted(t *testing.T, workers []worker.WorkerSnapshot, other parallelRunRef) {
	t.Helper()
	for _, slot := range workers {
		if slot.ID != other.WorkerID {
			continue
		}
		if slot.State != worker.WorkerBusy || slot.CurrentAssignmentID != "" || slot.ProfileKey != "" {
			t.Fatalf("other-run Worker was not redacted: %+v", slot)
		}
		return
	}
	t.Fatalf("other-run Worker %q missing", other.WorkerID)
}

func closeWorkerPoolForTest(t *testing.T, pool *worker.Pool) {
	t.Helper()
	pool.CancelRun(context.Background(), "run-visible", "cleanup")
	pool.CancelRun(context.Background(), "run-other", "cleanup")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pool.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockingRuntimeWorkerExecutor struct {
	started chan worker.ExecuteRequest
}

func (e *blockingRuntimeWorkerExecutor) Execute(ctx context.Context, request worker.ExecuteRequest, _ worker.EventSink) (worker.ExecuteResult, error) {
	e.started <- request
	<-ctx.Done()
	return worker.ExecuteResult{}, ctx.Err()
}

func (*blockingRuntimeWorkerExecutor) Cancel(context.Context, worker.AssignmentID, string) error {
	return nil
}
func (*blockingRuntimeWorkerExecutor) Reset(context.Context) error { return nil }
func (*blockingRuntimeWorkerExecutor) Close(context.Context) error { return nil }
func (*blockingRuntimeWorkerExecutor) Healthy() bool               { return true }

func waitForRuntimeWorkerStart(t *testing.T, started <-chan worker.ExecuteRequest, assignmentID worker.AssignmentID) {
	t.Helper()
	select {
	case request := <-started:
		if request.AssignmentID != assignmentID {
			t.Fatalf("started assignment = %s, want %s", request.AssignmentID, assignmentID)
		}
	case <-time.After(time.Second):
		t.Fatalf("assignment %s did not start", assignmentID)
	}
}

type reusableRuntimeProcess struct {
	mu       sync.Mutex
	healthy  bool
	closed   int
	lifetime context.Context
}

func (p *reusableRuntimeProcess) Start(context.Context, methods.ReplyParams, func(events.Envelope)) error {
	return nil
}

func (*reusableRuntimeProcess) Cancel(context.Context, string, string) error { return nil }

func (p *reusableRuntimeProcess) Close(context.Context) error {
	p.mu.Lock()
	p.closed++
	p.healthy = false
	p.mu.Unlock()
	return nil
}

func (p *reusableRuntimeProcess) Healthy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.healthy
}

func TestLazyProcessExecutorReusesHealthyProcessAndRebuildsDeadProcess(t *testing.T) {
	executor := newLazyProcessExecutor(nil, "worker-01")
	var mu sync.Mutex
	var created []*reusableRuntimeProcess
	executor.newProcess = func(ctx context.Context, _ methods.ReplyParams, _ string, _ func(context.Context, string, any) (json.RawMessage, error)) (worker.Process, error) {
		process := &reusableRuntimeProcess{healthy: true, lifetime: ctx}
		mu.Lock()
		created = append(created, process)
		mu.Unlock()
		return process, nil
	}

	first, err := executor.processFor(methods.ReplyParams{}, "assignment-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := executor.processFor(methods.ReplyParams{}, "assignment-2")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(created) != 1 {
		t.Fatalf("healthy process was not reused: first=%p second=%p created=%d", first, second, len(created))
	}
	select {
	case <-created[0].lifetime.Done():
		t.Fatal("process lifetime ended between assignments")
	default:
	}

	created[0].mu.Lock()
	created[0].healthy = false
	created[0].mu.Unlock()
	third, err := executor.processFor(methods.ReplyParams{}, "assignment-3")
	if err != nil {
		t.Fatal(err)
	}
	if third == first || len(created) != 2 {
		t.Fatalf("dead process was not rebuilt: first=%p third=%p created=%d", first, third, len(created))
	}
	if err := executor.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-created[1].lifetime.Done():
	case <-time.After(time.Second):
		t.Fatal("executor close did not cancel process lifetime")
	}
}
