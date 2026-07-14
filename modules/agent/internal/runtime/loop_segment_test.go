package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestFinishStatusFromLoopEnd(t *testing.T) {
	if got := finishStatusFromLoopEnd(loopEndNoTools); got != "completed" {
		t.Fatalf("no_tools -> %q", got)
	}
	if got := finishStatusFromLoopEnd(loopEndMaxTurns); got != "completed" {
		t.Fatalf("max_turns -> %q want completed (recoverable)", got)
	}
	if got := finishStatusFromLoopEnd(loopEndCancelled); got != "cancelled" {
		t.Fatalf("cancelled -> %q", got)
	}
	if got := finishStatusFromLoopEnd(loopEndFailed); got != "failed" {
		t.Fatalf("failed -> %q", got)
	}
	if got := finishStatusFromLoopEnd(loopEndBudget); got != "failed" {
		t.Fatalf("budget_exhausted -> %q", got)
	}
}

func TestCarrySummarizedHistoryKeepsTailAndTruncates(t *testing.T) {
	history := []ToolExchange{
		{Call: tools.Call{Name: "a"}, Result: tools.Result{Output: strings.Repeat("x", 20)}},
		{Call: tools.Call{Name: "b"}, Result: tools.Result{Output: strings.Repeat("y", 20)}},
		{Call: tools.Call{Name: "c"}, Result: tools.Result{Output: strings.Repeat("z", 20)}},
	}
	got := carrySummarizedHistory(history, 2, 5)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Call.Name != "b" || got[1].Call.Name != "c" {
		t.Fatalf("tail = %s,%s", got[0].Call.Name, got[1].Call.Name)
	}
	if got[0].Result.Output != "yyyyy…" {
		t.Fatalf("truncated = %q", got[0].Result.Output)
	}
	// 原始历史记录不变。
	if history[0].Result.Output != strings.Repeat("x", 20) {
		t.Fatal("source history mutated")
	}
}

// maxTurnsOnceProvider 总会继续请求工具，直到预算耗尽。
type maxTurnsOnceProvider struct {
	calls int
}

func (*maxTurnsOnceProvider) Name() string { return "max-turns-test" }

func (p *maxTurnsOnceProvider) Complete(_ context.Context, req provider.ProviderRequest, emit func(provider.ProviderChunk) error) error {
	p.calls++
	// 移除工具后（retryFinalAnswer）生成文本。
	if len(req.Tools) == 0 {
		_ = emit(provider.ProviderChunk{Delta: "synthesized after max turns"})
		return emit(provider.ProviderChunk{Final: true})
	}
	return emit(provider.ProviderChunk{ToolCalls: []tools.Call{{
		ID:        "tool_max_turns",
		Name:      "workspace.list",
		Risk:      tools.RiskLow,
		Arguments: map[string]any{"path": ".", "max_depth": 1},
	}}})
}

func TestRunProviderLoopSegmentReportsMaxTurnsReason(t *testing.T) {
	provider := &maxTurnsOnceProvider{}
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	rt.provider = provider

	params := methods.ReplyParams{
		RunID: "run_max_turns_seg",
		Session: methods.ReplySession{
			ID:         "session_max_turns",
			WorkingDir: t.TempDir(),
		},
		Input:   methods.ReplyInput{Text: "list forever"},
		Options: methods.ReplyOptions{MaxToolTurns: 2, EmitToolEvents: true},
	}
	// 注册以使事件发送路径正常工作。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !rt.registerRun(params.RunID, cancel) {
		t.Fatal("register run")
	}
	defer rt.unregisterRun(params.RunID)

	streamSeq := uint64(1)
	seg := rt.runProviderLoopSegment(ctx, params, params.Input.Text, nil, "msg_1", "stream_1", &streamSeq)
	if seg.Reason != loopEndMaxTurns {
		t.Fatalf("reason = %q, want max_turns (got tool_turns=%d calls=%d)", seg.Reason, seg.ToolTurns, provider.calls)
	}
	if seg.ToolTurns < 1 {
		t.Fatalf("tool turns = %d", seg.ToolTurns)
	}
}

func TestEmitRunFinishPayloadIncludesLoopEndReason(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = &echoFinishProvider{}

	sendRequest(t, context.Background(), rt, "run_loop_reason", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_loop_reason",
		Session: methods.ReplySession{
			ID:         "session_loop_reason",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "hello"},
	})
	waitForResponse(t, lines, "run_loop_reason")
	eventsOut := waitForEventsUntilFinish(t, lines)
	var finish events.EnvelopeV2
	for _, ev := range eventsOut {
		if ev.Type == events.EventFinish {
			finish = ev
			break
		}
	}
	if finish.EventID == "" {
		t.Fatal("missing finish event")
	}
	reason, _ := finish.Payload["loop_end_reason"].(string)
	if reason != string(loopEndNoTools) {
		t.Fatalf("loop_end_reason = %q, want no_tools payload=%#v", reason, finish.Payload)
	}
}

type echoFinishProvider struct{}

func (*echoFinishProvider) Name() string { return "echo-finish" }

func (*echoFinishProvider) Complete(_ context.Context, _ ProviderRequest, emit func(ProviderChunk) error) error {
	_ = emit(provider.ProviderChunk{Delta: "hi"})
	return emit(provider.ProviderChunk{Final: true})
}
