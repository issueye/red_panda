package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type healthCheckedProcess struct {
	pingErr error
	closed  bool
}

type blockingProcess struct{}

type outputHeavyProcess struct {
	text string
}

type capturingProcess struct {
	params chan methods.ReplyParams
}

func (blockingProcess) Start(ctx context.Context, _ methods.ReplyParams, _ func(events.Envelope)) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingProcess) Cancel(context.Context, string, string) error { return nil }
func (blockingProcess) Close(context.Context) error                  { return nil }

func (p outputHeavyProcess) Start(_ context.Context, params methods.ReplyParams, onEvent func(events.Envelope)) error {
	onEvent(events.Envelope{
		RootRunID: params.RunID,
		RunID:     params.RunID,
		Type:      events.EventMessageDelta,
		Payload:   map[string]any{"delta": p.text},
	})
	onEvent(events.Envelope{
		RootRunID: params.RunID,
		RunID:     params.RunID,
		Type:      events.EventFinish,
		Payload:   map[string]any{"status": "completed"},
	})
	return nil
}

func (outputHeavyProcess) Cancel(context.Context, string, string) error { return nil }
func (outputHeavyProcess) Close(context.Context) error                  { return nil }

func (p capturingProcess) Start(_ context.Context, params methods.ReplyParams, onEvent func(events.Envelope)) error {
	p.params <- params
	onEvent(events.Envelope{Type: events.EventMessageDelta, Payload: map[string]any{"delta": "bounded report"}})
	onEvent(events.Envelope{Type: events.EventFinish, Payload: map[string]any{"status": "completed"}})
	return nil
}

func (capturingProcess) Cancel(context.Context, string, string) error { return nil }
func (capturingProcess) Close(context.Context) error                  { return nil }

func (p *healthCheckedProcess) Start(context.Context, methods.ReplyParams, func(events.Envelope)) error {
	return nil
}

func (p *healthCheckedProcess) Cancel(context.Context, string, string) error { return nil }

func (p *healthCheckedProcess) Close(context.Context) error {
	p.closed = true
	return nil
}

func (p *healthCheckedProcess) Ping(context.Context) error { return p.pingErr }

func TestExecuteToolBatchPreservesBarriersBetweenWorkerGroups(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	var mu sync.Mutex
	var order []string
	firstGroupReady := make(chan struct{})
	releaseFirstGroup := make(chan struct{})
	startedFirst := 0

	rt.tools.SubagentExecutor = func(ctx context.Context, _ ToolRunContext, call tools.Call) (string, error) {
		name, _ := call.Arguments["name"].(string)
		mu.Lock()
		order = append(order, "start:"+name)
		if name == "a" || name == "b" {
			startedFirst++
			if startedFirst == 2 {
				close(firstGroupReady)
			}
		}
		mu.Unlock()

		if name == "a" || name == "b" {
			select {
			case <-releaseFirstGroup:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		mu.Lock()
		order = append(order, "finish:"+name)
		mu.Unlock()
		return name + " done", nil
	}
	rt.tools.GoalExecutor = func(_ context.Context, _ methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
		mu.Lock()
		order = append(order, "goal.update")
		mu.Unlock()
		return methods.GoalToolExecuteResult{Status: "completed"}, nil
	}

	calls := []tools.Call{
		{ID: "worker_a", Name: "subagent.run", Arguments: map[string]any{"name": "a", "task": "a"}},
		{ID: "worker_b", Name: "subagent.run", Arguments: map[string]any{"name": "b", "task": "b"}},
		{ID: "goal_update", Name: "goal.update", Arguments: map[string]any{"status": "active"}},
		{ID: "worker_c", Name: "subagent.run", Arguments: map[string]any{"name": "c", "task": "c"}},
	}
	params := methods.ReplyParams{
		RunID:   "run_worker_order",
		Session: methods.ReplySession{ID: "session_worker_order"},
		Options: methods.ReplyOptions{ToolPolicy: "allow_all"},
	}

	done := make(chan []ToolExchange, 1)
	go func() {
		history, _ := rt.executeToolBatch(context.Background(), params, calls)
		done <- history
	}()

	select {
	case <-firstGroupReady:
	case <-time.After(time.Second):
		t.Fatal("adjacent workers did not start concurrently")
	}

	mu.Lock()
	for _, item := range order {
		if item == "goal.update" || item == "start:c" {
			mu.Unlock()
			t.Fatalf("ordering barrier crossed before first worker group completed: %v", order)
		}
	}
	mu.Unlock()
	close(releaseFirstGroup)

	var history []ToolExchange
	select {
	case history = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tool batch did not finish")
	}

	if len(history) != len(calls) {
		t.Fatalf("history length = %d, want %d", len(history), len(calls))
	}
	for i, call := range calls {
		if history[i].Call.ID != call.ID {
			t.Fatalf("history[%d] call = %s, want %s", i, history[i].Call.ID, call.ID)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	goalUpdateIndex := indexOfString(order, "goal.update")
	workerCIndex := indexOfString(order, "start:c")
	if goalUpdateIndex < 0 || workerCIndex < 0 || goalUpdateIndex > workerCIndex {
		t.Fatalf("ordinary tool did not remain before the following worker group: %v", order)
	}
	for _, name := range []string{"finish:a", "finish:b"} {
		if finishIndex := indexOfString(order, name); finishIndex < 0 || finishIndex > goalUpdateIndex {
			t.Fatalf("first worker group did not finish before ordinary tool: %v", order)
		}
	}
}

func TestExecuteToolBatchCancellationStopsBeforeFollowingOrdinaryTool(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	workerStarted := make(chan struct{})
	goalCalled := make(chan struct{}, 1)
	rt.tools.SubagentExecutor = func(ctx context.Context, _ ToolRunContext, _ tools.Call) (string, error) {
		close(workerStarted)
		<-ctx.Done()
		return "", ctx.Err()
	}
	rt.tools.GoalExecutor = func(context.Context, methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
		goalCalled <- struct{}{}
		return methods.GoalToolExecuteResult{Status: "completed"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rt.executeToolBatch(ctx, methods.ReplyParams{
			RunID:   "run_cancelled_batch",
			Options: methods.ReplyOptions{ToolPolicy: "allow_all"},
		}, []tools.Call{
			{ID: "worker", Name: "subagent.run", Arguments: map[string]any{"name": "worker", "task": "wait"}},
			{ID: "goal", Name: "goal.update", Arguments: map[string]any{"status": "active"}},
		})
	}()
	select {
	case <-workerStarted:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled batch did not return")
	}
	select {
	case <-goalCalled:
		t.Fatal("ordinary tool ran after worker batch cancellation")
	default:
	}
}

func indexOfString(items []string, target string) int {
	for index, item := range items {
		if item == target {
			return index
		}
	}
	return -1
}

func TestSubAgentProcessPoolAcquiresQueuedWorkersFIFO(t *testing.T) {
	pool := newSubAgentProcessPool(1, func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return fakeProcessSubAgent{}, nil
	})
	_, releaseFirst, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "first")
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan string, 2)
	releaseSecond := make(chan struct{})
	releaseThird := make(chan struct{})
	queue := func(id string, release <-chan struct{}) {
		_, done, acquireErr := pool.Acquire(context.Background(), methods.ReplyParams{}, id)
		if acquireErr != nil {
			acquired <- id + ":" + acquireErr.Error()
			return
		}
		acquired <- id
		<-release
		done(true)
	}

	go queue("second", releaseSecond)
	waitForPoolQueued(t, pool, 1)
	go queue("third", releaseThird)
	waitForPoolQueued(t, pool, 2)

	releaseFirst(true)
	if got := <-acquired; got != "second" {
		t.Fatalf("first queued acquisition = %q, want second", got)
	}
	close(releaseSecond)
	if got := <-acquired; got != "third" {
		t.Fatalf("second queued acquisition = %q, want third", got)
	}
	close(releaseThird)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := pool.Status()
		if status.Queued == 0 && status.InUse == 0 && status.Idle == 1 {
			if status.TotalWaits < 3 {
				t.Fatalf("total waits = %d, want at least 3 grants", status.TotalWaits)
			}
			if status.MaxWaitMS <= 0 || status.LastWaitMS <= 0 {
				t.Fatalf("wait metrics were not recorded: %#v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pool did not settle: %#v", pool.Status())
}

func TestSubAgentProcessPoolCancelsQueuedWorkerWithoutLeakingSlot(t *testing.T) {
	pool := newSubAgentProcessPool(1, func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return fakeProcessSubAgent{}, nil
	})
	_, release, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "holder")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, _, acquireErr := pool.Acquire(ctx, methods.ReplyParams{}, "cancelled")
		result <- acquireErr
	}()
	waitForPoolQueued(t, pool, 1)
	cancel()
	if err := <-result; err != context.Canceled {
		t.Fatalf("queued acquire error = %v, want context.Canceled", err)
	}
	status := pool.Status()
	if status.Queued != 0 || status.Active != 1 || status.InUse != 1 {
		t.Fatalf("cancelled waiter leaked capacity: %#v", status)
	}
	release(false)
	if status = pool.Status(); status.Active != 0 || status.InUse != 0 {
		t.Fatalf("released pool did not settle: %#v", status)
	}
}

func TestSubAgentProcessPoolReleaseIsIdempotent(t *testing.T) {
	pool := newSubAgentProcessPool(1, func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return fakeProcessSubAgent{}, nil
	})
	_, release, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "worker")
	if err != nil {
		t.Fatal(err)
	}
	release(false)
	release(false)
	if status := pool.Status(); status.Active != 0 || status.InUse != 0 {
		t.Fatalf("double release corrupted counters: %#v", status)
	}
}

func TestSubAgentProcessPoolReleaseStaysSafeAcrossResizeAndReset(t *testing.T) {
	pool := newSubAgentProcessPool(2, func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return fakeProcessSubAgent{}, nil
	})
	_, releaseFirst, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "first")
	if err != nil {
		t.Fatal(err)
	}
	_, releaseSecond, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "second")
	if err != nil {
		t.Fatal(err)
	}
	pool.SetLimit(1)
	pool.Reset(context.Background())
	releaseFirst(true)
	releaseFirst(true)
	releaseSecond(true)
	releaseSecond(true)
	status := pool.Status()
	if status.Limit != 1 || status.Active != 1 || status.Idle != 1 || status.InUse != 0 || status.Queued != 0 {
		t.Fatalf("release after resize/reset corrupted pool state: %#v", status)
	}
}

func TestSubAgentProcessPoolReplacesUnhealthyIdleWorker(t *testing.T) {
	created := 0
	stale := &healthCheckedProcess{pingErr: errors.New("process exited")}
	fresh := &healthCheckedProcess{}
	pool := newSubAgentProcessPool(1, func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		created++
		if created == 1 {
			return stale, nil
		}
		return fresh, nil
	})
	_, release, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "first")
	if err != nil {
		t.Fatal(err)
	}
	release(true)
	child, releaseFresh, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "second")
	if err != nil {
		t.Fatal(err)
	}
	gotFresh, ok := child.(*healthCheckedProcess)
	if !ok || gotFresh != fresh || created != 2 || !stale.closed {
		t.Fatalf("stale worker was not replaced: child=%p created=%d staleClosed=%v", child, created, stale.closed)
	}
	releaseFresh(false)
}

func TestApplyCustomAgentDefinitionEnforcesPromptToolsAndTurns(t *testing.T) {
	child := methods.ReplyParams{Options: methods.ReplyOptions{
		ToolAllowlist: []string{"workspace.read_file", "shell.exec"},
	}}
	definition := methods.AgentDefinitionSnapshot{
		Key:             "reviewer",
		SystemPrompt:    "Review code with evidence.",
		ToolAllowlist:   []string{"workspace.read_file"},
		ToolDenylist:    []string{"workspace.write_file"},
		DefaultMaxTurns: 7,
		Enabled:         true,
	}
	turns := applyAgentDefinition(&child, definition, "review auth", 7)
	if turns != 7 || child.Options.MaxToolTurns != 7 {
		t.Fatalf("custom turns not applied: turns=%d options=%d", turns, child.Options.MaxToolTurns)
	}
	if len(child.Options.ToolAllowlist) != 1 || child.Options.ToolAllowlist[0] != "workspace.read_file" {
		t.Fatalf("custom allowlist not enforced: %#v", child.Options.ToolAllowlist)
	}
	if !containsString(child.Options.ToolDenylist, "workspace.write_file") {
		t.Fatalf("custom denylist not enforced: %#v", child.Options.ToolDenylist)
	}
	if child.Options.MemoryContext == nil || !strings.Contains(child.Options.MemoryContext.Context, "Review code with evidence") {
		t.Fatalf("custom prompt not injected: %#v", child.Options.MemoryContext)
	}
}

func TestGoalSpecialistCannotExceedGatewayWorkerTurnLimit(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	childParams := make(chan methods.ReplyParams, 1)
	rt.newProcessSubAgent = func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return capturingProcess{params: childParams}, nil
	}
	rt.setRunGoal("run_goal_worker_cap", &runGoalState{
		Goal:           methods.GoalDTO{ID: "goal_cap", Status: "active", PipelinePhase: "analyze"},
		BoundToThisRun: true,
	})
	params := methods.ReplyParams{
		RunID: "run_goal_worker_cap",
		Options: methods.ReplyOptions{
			SubAgentBackend: "runtime_process",
			ToolPolicy:      "allow_all",
			WorkerMaxTurns:  5,
			AgentDefinitions: []methods.AgentDefinitionSnapshot{{
				Key: "goal-analyst", Enabled: true, DefaultMaxTurns: 30,
			}},
		},
	}
	if _, err := rt.executeSubagentRun(context.Background(), ToolRunContext{Reply: &params}, tools.Call{
		Name: "subagent.run", Arguments: map[string]any{"name": "goal-analyst", "task": "analyze"},
	}); err != nil {
		t.Fatal(err)
	}
	child := <-childParams
	if child.Options.MaxToolTurns != 5 {
		t.Fatalf("specialist max turns = %d, want Gateway cap 5", child.Options.MaxToolTurns)
	}
}

func TestSubagentRunRejectsDisabledAgent(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	params := methods.ReplyParams{
		RunID: "run_disabled_agent",
		Options: methods.ReplyOptions{AgentDefinitions: []methods.AgentDefinitionSnapshot{{
			Key: "reviewer", Enabled: false,
		}}},
	}
	_, err := rt.executeSubagentRun(context.Background(), ToolRunContext{Reply: &params}, tools.Call{
		Name: "subagent.run", Arguments: map[string]any{"name": "reviewer", "task": "review"},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled agent error = %v", err)
	}
}

func TestSubagentRunEnforcesWallTimeAndFanOut(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	rt.newProcessSubAgent = func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return blockingProcess{}, nil
	}
	params := methods.ReplyParams{
		RunID: "run_worker_budget",
		Options: methods.ReplyOptions{
			SubAgentBackend: "runtime_process",
			ToolPolicy:      "allow_all",
			WorkerMaxWallMS: 10,
			WorkerMaxFanOut: 1,
		},
	}
	call := tools.Call{Name: "subagent.run", Arguments: map[string]any{"name": "worker", "task": "wait"}}
	_, err := rt.executeSubagentRun(context.Background(), ToolRunContext{Reply: &params}, call)
	if err == nil || !strings.Contains(err.Error(), "wall-time budget exhausted") {
		t.Fatalf("wall-time error = %v", err)
	}
	_, err = rt.executeSubagentRun(context.Background(), ToolRunContext{Reply: &params}, call)
	if err == nil || !strings.Contains(err.Error(), "fan-out limit") {
		t.Fatalf("fan-out error = %v", err)
	}
}

func TestSubagentRunBoundsCapturedOutput(t *testing.T) {
	rt := New(nil, io.Discard, io.Discard, "test")
	fullOutput := strings.Repeat("x", maxToolOutputBytes+4096)
	rt.newProcessSubAgent = func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return outputHeavyProcess{text: fullOutput}, nil
	}
	params := methods.ReplyParams{
		RunID: "run_worker_output_budget",
		Options: methods.ReplyOptions{
			SubAgentBackend: "runtime_process",
			ToolPolicy:      "allow_all",
		},
	}
	result, err := rt.executeSubagentRun(context.Background(), ToolRunContext{Reply: &params}, tools.Call{
		Name: "subagent.run", Arguments: map[string]any{"name": "worker", "task": "produce report"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result, "\n[truncated]") {
		t.Fatalf("worker output was not marked truncated: len=%d", len(result))
	}
	if len(result) > maxToolOutputBytes+len("\n[truncated]") {
		t.Fatalf("worker output length = %d, want <= %d", len(result), maxToolOutputBytes+len("\n[truncated]"))
	}
}

func waitForPoolQueued(t *testing.T, pool *subAgentProcessPool, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pool.Status().Queued == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("queued = %d, want %d; status=%#v", pool.Status().Queued, want, pool.Status())
}
