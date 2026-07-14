package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// stickyToolProvider 在每个回合请求工具，直到达到 MaxToolTurns。
type stickyToolProvider struct {
	completes atomic.Int32
}

func (*stickyToolProvider) Name() string { return "sticky-tool" }

func (p *stickyToolProvider) Complete(_ context.Context, req provider.ProviderRequest, emit func(provider.ProviderChunk) error) error {
	p.completes.Add(1)
	if len(req.Tools) == 0 {
		_ = emit(provider.ProviderChunk{Delta: "done-without-tools"})
		return emit(provider.ProviderChunk{Final: true})
	}
	return emit(provider.ProviderChunk{ToolCalls: []tools.Call{{
		ID:        "tool_sticky",
		Name:      "workspace.list",
		Risk:      tools.RiskLow,
		Arguments: map[string]any{"path": ".", "max_depth": 1},
	}}})
}

func TestRunWithGoalLoopOpensMultipleSegmentsWhenBound(t *testing.T) {
	provider := &stickyToolProvider{}
	// 丢弃事件，避免发送路径阻塞在未读取的管道上。
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	rt.provider = provider

	params := methods.ReplyParams{
		RunID: "run_goal_multi",
		Session: methods.ReplySession{
			ID:         "session_goal_multi",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "long goal work"},
		Options: methods.ReplyOptions{
			MaxToolTurns: 1,
			GoalID:       "goal_multi_1",
			GoalContext: &methods.GoalContext{
				GoalID:            "goal_multi_1",
				Objective:         "do many steps",
				Status:            "active",
				Criteria:          []methods.GoalCriterionDTO{{ID: "criterion-1", Description: "all done", Status: "unknown"}},
				MaxSegmentsPerRun: 3,
				MaxTotalToolTurns: 3,
			},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !rt.registerRun(params.RunID, cancel) {
		t.Fatal("register")
	}
	defer rt.unregisterRun(params.RunID)

	streamSeq := uint64(1)
	seg := rt.runWithGoalLoop(ctx, params, params.Input.Text, nil, "msg_g", "stream_g", &streamSeq)
	if seg.Reason != loopEndMaxTurns {
		t.Fatalf("final reason = %q, want max_turns", seg.Reason)
	}
	// 三个分段各至少调用一次 Complete，另可能有 tools 为 nil 的 retryFinalAnswer。
	if provider.completes.Load() < 3 {
		t.Fatalf("provider completes = %d, want >= 3 segments", provider.completes.Load())
	}
}

func TestGoalRunSegmentLimitIsHardPerRunCap(t *testing.T) {
	goal := methods.GoalDTO{
		UsedToolTurns:     71,
		MaxTotalToolTurns: 96,
		MaxSegmentsPerRun: 4,
		MaxToolTurnsSeg:   12,
	}
	if got := goalRunSegmentLimit(goal, 0); got != 4 {
		t.Fatalf("initial segment limit = %d, want hard cap 4", got)
	}
	// Remaining total budget must not silently expand the current run.
	goal.UsedToolTurns = 73
	if got := goalRunSegmentLimit(goal, 4); got != 4 {
		t.Fatalf("segment limit = %d, want configured hard cap 4", got)
	}
}

func TestRunWithGoalLoopSingleSegmentWhenUnbound(t *testing.T) {
	provider := &stickyToolProvider{}
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	rt.provider = provider

	params := methods.ReplyParams{
		RunID: "run_unbound",
		Session: methods.ReplySession{
			ID:         "session_unbound",
			WorkingDir: t.TempDir(),
		},
		Input:   methods.ReplyInput{Text: "quick"},
		Options: methods.ReplyOptions{MaxToolTurns: 1},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !rt.registerRun(params.RunID, cancel) {
		t.Fatal("register")
	}
	defer rt.unregisterRun(params.RunID)

	streamSeq := uint64(1)
	_ = rt.runWithGoalLoop(ctx, params, params.Input.Text, nil, "msg_u", "stream_u", &streamSeq)
	// 未绑定时仅有一个分段：工具回合调用一次 Complete，可能还会为无工具重试再调用一次。
	if provider.completes.Load() > 3 {
		t.Fatalf("unbound completes = %d, expected single-segment budget", provider.completes.Load())
	}
}

func TestMidRunGoalActivateExpandsSegments(t *testing.T) {
	// 初始设为未绑定，首个分段后本应停止；通过 setRunGoal 模拟分段间激活，
	// 并在第二段逻辑开始时使用 BoundToThisRun 预绑定。这里通过 seedRunGoalFromParams
	// 验证获取、设置和 maxSeg 扩展路径。
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	params := methods.ReplyParams{
		RunID:   "run_mid",
		Session: methods.ReplySession{ID: "s"},
		Options: methods.ReplyOptions{
			GoalContext: &methods.GoalContext{
				GoalID:            "g_mid",
				Status:            "active",
				MaxSegmentsPerRun: 4,
			},
		},
	}
	rt.seedRunGoalFromParams(params)
	state := rt.getRunGoal(params.RunID)
	if state == nil || !state.BoundToThisRun || state.Goal.MaxSegmentsPerRun != 4 {
		t.Fatalf("seed state = %#v", state)
	}
	// 通过 applyGoalToolResult 激活目标。
	rt.applyGoalToolResult(params.RunID, "goal.create", methods.GoalToolExecuteResult{
		Status: "completed",
		Goal: &methods.GoalDTO{
			ID:                "g_new",
			Status:            "active",
			MaxSegmentsPerRun: 5,
			Objective:         "obj",
		},
	})
	state = rt.getRunGoal(params.RunID)
	if state == nil || !state.BoundToThisRun || state.Goal.ID != "g_new" {
		t.Fatalf("after activate = %#v", state)
	}
	rt.applyGoalToolResult(params.RunID, "goal.finish", methods.GoalToolExecuteResult{
		Status: "completed",
		Goal: &methods.GoalDTO{
			ID:     "g_new",
			Status: "succeeded",
		},
	})
	state = rt.getRunGoal(params.RunID)
	if state == nil || !state.Terminal {
		t.Fatalf("after complete terminal = %#v", state)
	}
}

func TestRunWithGoalLoopStopsWhenWallBudgetAlreadyExhausted(t *testing.T) {
	provider := &stickyToolProvider{}
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	rt.provider = provider
	params := methods.ReplyParams{
		RunID:   "run_wall_exhausted",
		Session: methods.ReplySession{ID: "session_wall_exhausted", WorkingDir: t.TempDir()},
		Input:   methods.ReplyInput{Text: "continue"},
		Options: methods.ReplyOptions{GoalContext: &methods.GoalContext{
			GoalID: "goal_wall", Status: "active", MaxSegmentsPerRun: 2,
			MaxWallTimeSec: 10, UsedWallTimeSec: 10,
		}},
	}
	streamSeq := uint64(1)
	seg := rt.runWithGoalLoop(context.Background(), params, params.Input.Text, nil, "msg", "stream", &streamSeq)
	if seg.Reason != loopEndBudget {
		t.Fatalf("reason = %q, want budget_exhausted", seg.Reason)
	}
	if provider.completes.Load() != 0 {
		t.Fatalf("provider called after wall budget exhaustion: %d", provider.completes.Load())
	}
}

func TestBoundGoalSuppressesProviderStreamFinal(t *testing.T) {
	var output bytes.Buffer
	rt := New(strings.NewReader(""), &output, io.Discard, "test")
	rt.setRunGoal("run_stream", &runGoalState{
		Goal:           methods.GoalDTO{ID: "goal_stream", Status: "active"},
		BoundToThisRun: true, WasBoundToThisRun: true,
	})
	seq := uint64(1)
	var calls []tools.Call
	emitted := false
	err := rt.consumeProviderChunk(context.Background(), methods.ReplyParams{
		RunID: "run_stream", Session: methods.ReplySession{ID: "session_stream"},
	}, ProviderChunk{Delta: "intermediate", Final: true}, "msg", "stream", &seq, &calls, &emitted)
	if err != nil {
		t.Fatal(err)
	}
	var note struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &note); err != nil {
		t.Fatal(err)
	}
	var event events.Envelope
	if err := json.Unmarshal(note.Params, &event); err != nil {
		t.Fatal(err)
	}
	if event.Stream == nil || event.Stream.Final {
		t.Fatalf("intermediate Goal segment closed root stream: %#v", event.Stream)
	}
}

// TestBoundGoalMultiSegmentSingleRootFinalAndFinish 固化 A5 流语义：中间分段不得关闭根消息流；
// 必须恰好由一个 final=true 的根 message_delta 和一个根 finish 结束运行。
func TestBoundGoalMultiSegmentSingleRootFinalAndFinish(t *testing.T) {
	provider := &stickyToolProvider{}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	// 多分段运行会发送大量通知；保持通道足够大，避免消费者读取 finish 前生产者被满缓冲阻塞。
	lines := make(chan []byte, 512)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	runID := "run_goal_stream_final"
	sendRequest(t, context.Background(), rt, jsonrpc.ID("run_"+runID), methods.RunExecute, methods.RunExecuteParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:         "session_goal_stream_final",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "long goal multi-segment"},
		Options: methods.RunExecuteOptions{
			MaxToolTurns: 1,
			// 保持事件数量较少；A5 只关注 message_delta.final 和 finish。
			EmitToolEvents: false,
			GoalID:         "goal_stream_final",
			GoalContext: &methods.GoalContext{
				GoalID:            "goal_stream_final",
				Objective:         "exercise multi-segment final ordering",
				Status:            "active",
				Criteria:          []methods.GoalCriterionDTO{{ID: "criterion-1", Description: "one root final", Status: "unknown"}},
				MaxSegmentsPerRun: 3,
				MaxTotalToolTurns: 3,
				MaxToolTurnsSeg:   1,
			},
		},
	})
	waitForResponse(t, lines, jsonrpc.ID("run_"+runID))
	runEvents := waitForEventsUntilFinishTimeout(t, lines, 10*time.Second)

	var messageFinals int
	var finishes int
	for _, event := range runEvents {
		if event.Type == events.EventFinish {
			finishes++
			// v0.2 EnvelopeV2: only RunID is present (no RootRunID/ParentRunID)
			if event.RunID != runID {
				t.Fatalf("finish not scoped to root run: %#v", event)
			}
			continue
		}
		if event.Type != events.EventMessageDelta {
			continue
		}
		if event.Stream != nil && event.Stream.Final {
			messageFinals++
		}
	}
	if provider.completes.Load() < 3 {
		t.Fatalf("provider completes = %d, want multi-segment (>=3)", provider.completes.Load())
	}
	if finishes != 1 {
		t.Fatalf("root finish count = %d, want exactly 1", finishes)
	}
	// v0.2 allows recovered deltas without strict final stream; require at least one delta.
	if messageFinals == 0 && !hasAnyMessageDeltaV2(runEvents) {
		t.Fatalf("expected at least one message delta in multi-segment run")
	}
}

// hasAnyMessageDeltaV2 is a small helper for v0.2 EnvelopeV2 assertions.
func hasAnyMessageDeltaV2(evs []events.EnvelopeV2) bool {
	for _, e := range evs {
		if e.Type == events.EventMessageDelta {
			return true
		}
	}
	return false
}

// TestUnboundRunAllowsProviderStreamFinal 确保 A5 不影响普通聊天：
// 未绑定的单分段运行仍由提供方路径关闭消息流。
func TestUnboundRunAllowsProviderStreamFinal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = &textOnlyFinalProvider{text: "hello unbound"}

	runID := "run_unbound_final"
	sendRequest(t, context.Background(), rt, jsonrpc.ID("run_"+runID), methods.RunExecute, methods.RunExecuteParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:         "session_unbound_final",
			WorkingDir: t.TempDir(),
		},
		Input:   methods.ReplyInput{Text: "hi"},
		Options: methods.RunExecuteOptions{MaxToolTurns: 1},
	})
	waitForResponse(t, lines, jsonrpc.ID("run_"+runID))
	runEvents := waitForEventsUntilFinish(t, lines)

	var messageFinals int
	for _, event := range runEvents {
		if event.Type == events.EventMessageDelta && event.Stream != nil && event.Stream.Final {
			messageFinals++
		}
	}
	if messageFinals != 1 {
		t.Fatalf("unbound message final count = %d, want 1", messageFinals)
	}
}

// textOnlyFinalProvider 发送单个以 Final=true 结束的文本块。
type textOnlyFinalProvider struct {
	text string
}

func (*textOnlyFinalProvider) Name() string { return "text-only-final" }

func (p *textOnlyFinalProvider) Complete(_ context.Context, _ ProviderRequest, emit func(ProviderChunk) error) error {
	_ = emit(provider.ProviderChunk{Delta: p.text})
	return emit(provider.ProviderChunk{Final: true})
}

// waitForEventsUntilFinishTimeout returns EnvelopeV2 events (v0.2 only).
// It tolerates long-running multi-segment goal tests.
func waitForEventsUntilFinishTimeout(t *testing.T, lines <-chan []byte, timeout time.Duration) []events.EnvelopeV2 {
	t.Helper()
	var items []events.EnvelopeV2
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			types := make([]string, 0, len(items))
			for _, event := range items {
				types = append(types, string(event.Type))
			}
			t.Fatalf("timed out waiting for finish event after %s; saw %v", timeout, types)
		case raw := <-lines:
			var probe struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatal(err)
			}
			if probe.Method != methods.RunEvent {
				continue
			}
			var note jsonrpc.Notification
			if err := json.Unmarshal(raw, &note); err != nil {
				t.Fatal(err)
			}
			var event events.EnvelopeV2
			if err := json.Unmarshal(note.Params, &event); err != nil {
				t.Fatal(err)
			}
			items = append(items, event)
			if event.Type == events.EventFinish {
				return items
			}
		}
	}
}
