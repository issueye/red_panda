package subagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

type coordinatorProcess struct {
	start func(context.Context, methods.ReplyParams, func(events.Envelope)) error
}

func (p coordinatorProcess) Start(ctx context.Context, params methods.ReplyParams, emit func(events.Envelope)) error {
	return p.start(ctx, params, emit)
}

func (coordinatorProcess) Cancel(context.Context, string, string) error { return nil }
func (coordinatorProcess) Close(context.Context) error                  { return nil }

type recordingProcessProvider struct {
	process  Process
	err      error
	params   methods.ReplyParams
	backend  string
	released []bool
}

func (p *recordingProcessProvider) Acquire(_ context.Context, params methods.ReplyParams, _ string, backend string) (Process, ReleaseFunc, error) {
	p.params = params
	p.backend = backend
	if p.err != nil {
		return nil, nil, p.err
	}
	return p.process, func(reusable bool) {
		p.released = append(p.released, reusable)
	}, nil
}

type recordingEventSink struct {
	mu     sync.Mutex
	order  []string
	status []StatusEvent
}

func (s *recordingEventSink) EmitStatus(_ context.Context, _ RunSpec, status StatusEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "status:"+status.Status)
	s.status = append(s.status, status)
	return nil
}

func (s *recordingEventSink) Bridge(_ context.Context, _ RunSpec, event events.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "event:"+string(event.Type))
	return nil
}

func TestCoordinatorRunsLifecycleAndReusesSuccessfulProcess(t *testing.T) {
	registry := NewRegistry()
	sink := &recordingEventSink{}
	provider := &recordingProcessProvider{process: coordinatorProcess{start: func(_ context.Context, params methods.ReplyParams, emit func(events.Envelope)) error {
		if params.RunID != "child-run" {
			t.Fatalf("child run id = %q", params.RunID)
		}
		emit(events.Envelope{Type: events.EventMessageDelta, Payload: map[string]any{"delta": "final report"}})
		emit(events.Envelope{Type: events.EventFinish, Payload: map[string]any{"status": "completed"}})
		return nil
	}}}
	coordinator, err := NewCoordinator(provider, sink, registry)
	if err != nil {
		t.Fatal(err)
	}
	spec := testRunSpec()
	result, err := coordinator.Run(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "final report" || result.FinishStatus != "completed" {
		t.Fatalf("result = %#v", result)
	}
	if provider.backend != "process_pool" || provider.params.RunID != "child-run" {
		t.Fatalf("provider input = backend %q params %#v", provider.backend, provider.params)
	}
	if len(provider.released) != 1 || !provider.released[0] {
		t.Fatalf("released = %#v", provider.released)
	}
	wantOrder := []string{
		"status:running",
		"event:message_delta",
		"event:finish",
		"status:completed",
	}
	if strings.Join(sink.order, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("event order = %#v", sink.order)
	}
	record, ok := registry.Lookup("root-run", "worker-1")
	if !ok || record.Status != "completed" {
		t.Fatalf("registry record = %#v, ok=%v", record, ok)
	}
}

func TestCoordinatorDoesNotReuseFailedProcess(t *testing.T) {
	registry := NewRegistry()
	sink := &recordingEventSink{}
	provider := &recordingProcessProvider{process: coordinatorProcess{start: func(context.Context, methods.ReplyParams, func(events.Envelope)) error {
		return errors.New("start failed")
	}}}
	coordinator, err := NewCoordinator(provider, sink, registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Run(context.Background(), testRunSpec())
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("error = %v", err)
	}
	if len(provider.released) != 1 || provider.released[0] {
		t.Fatalf("released = %#v", provider.released)
	}
	record, ok := registry.Lookup("root-run", "worker-1")
	if !ok || record.Status != "failed" {
		t.Fatalf("registry record = %#v, ok=%v", record, ok)
	}
}

func TestCoordinatorMarksCancelledContext(t *testing.T) {
	registry := NewRegistry()
	sink := &recordingEventSink{}
	provider := &recordingProcessProvider{process: coordinatorProcess{start: func(ctx context.Context, _ methods.ReplyParams, _ func(events.Envelope)) error {
		<-ctx.Done()
		return ctx.Err()
	}}}
	coordinator, err := NewCoordinator(provider, sink, registry)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = coordinator.Run(ctx, testRunSpec())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	record, ok := registry.Lookup("root-run", "worker-1")
	if !ok || record.Status != "cancelled" {
		t.Fatalf("registry record = %#v, ok=%v", record, ok)
	}
}

func testRunSpec() RunSpec {
	return RunSpec{
		RootRunID:   "root-run",
		SubAgentID:  "worker-1",
		Name:        "worker",
		DisplayName: "Worker",
		Backend:     "process_pool",
		Task:        "analyze files",
		MaxTurns:    16,
		Parent: methods.ReplyParams{
			RunID:   "root-run",
			Session: methods.ReplySession{ID: "session-1"},
		},
		Child: methods.ReplyParams{RunID: "child-run"},
	}
}
