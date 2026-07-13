package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"redpanda/agent/internal/subagent"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

type adapterTestProcess struct {
	closed int
}

func (*adapterTestProcess) Start(context.Context, methods.ReplyParams, func(events.Envelope)) error {
	return nil
}

func (*adapterTestProcess) Cancel(context.Context, string, string) error { return nil }

func (p *adapterTestProcess) Close(context.Context) error {
	p.closed++
	return nil
}

func TestSubagentAdapterProcessProviderSelectsBackend(t *testing.T) {
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	pooled := &adapterTestProcess{}
	rt.processPool = subagent.NewProcessPool(1, func(context.Context, methods.ReplyParams, string) (subagent.Process, error) {
		return pooled, nil
	})
	oneShot := &adapterTestProcess{}
	rt.newProcessSubAgent = func(context.Context, methods.ReplyParams, string) (subagent.Process, error) {
		return oneShot, nil
	}
	provider := runtimeProcessProvider{runtime: rt}

	child, release, err := provider.Acquire(context.Background(), methods.ReplyParams{}, "s1", "process_pool")
	if err != nil || child != pooled || release == nil {
		t.Fatalf("pool acquire: child=%v release=%v err=%v", child, release, err)
	}
	release(true)
	if status := rt.processPool.Status(); status.Idle != 1 {
		t.Fatalf("pool status = %#v", status)
	}

	child, release, err = provider.Acquire(context.Background(), methods.ReplyParams{}, "s2", "runtime_process")
	if err != nil || child != oneShot || release == nil {
		t.Fatalf("one-shot acquire: child=%v release=%v err=%v", child, release, err)
	}
	release(true)
	if oneShot.closed != 1 {
		t.Fatalf("one-shot close count = %d", oneShot.closed)
	}

	if _, _, err := provider.Acquire(context.Background(), methods.ReplyParams{}, "s3", "unknown"); err == nil {
		t.Fatal("unknown backend must fail")
	}
}

func TestSubagentAdapterEventSinkEmitsStatusAndBridgesChildEvent(t *testing.T) {
	var output bytes.Buffer
	rt := New(strings.NewReader(""), &output, &bytes.Buffer{}, "test")
	sink := runtimeEventSink{runtime: rt}
	spec := subagent.RunSpec{
		RootRunID:   "root-run",
		SubAgentID:  "worker-1",
		Name:        "goal-analyst",
		DisplayName: "分析专家",
		Backend:     "process_pool",
		GoalPhase:   "analyze",
		Parent: methods.ReplyParams{
			RunID:   "root-run",
			Session: methods.ReplySession{ID: "session-1"},
		},
	}
	if err := sink.EmitStatus(context.Background(), spec, subagent.StatusEvent{Status: "running", Summary: "started"}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Bridge(context.Background(), spec, events.Envelope{
		Type:   events.EventMessageDelta,
		Stream: &events.StreamRef{StreamID: "child-stream", Kind: events.StreamMessage, Seq: 1},
		Payload: map[string]any{
			"delta": "child output",
		},
	}); err != nil {
		t.Fatal(err)
	}
	beforeFinish := output.Len()
	if err := sink.Bridge(context.Background(), spec, events.Envelope{Type: events.EventFinish}); err != nil {
		t.Fatal(err)
	}
	if output.Len() != beforeFinish {
		t.Fatal("child finish event must not be bridged")
	}

	text := output.String()
	for _, want := range []string{
		`"subagent_id":"worker-1"`,
		`"status":"running"`,
		`"display_name":"分析专家"`,
		`"goal_phase":"analyze"`,
		`"stream_worker-1_child-stream"`,
		`"delta":"child output"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
