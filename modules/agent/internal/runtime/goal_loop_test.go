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

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// stickyToolProvider requests a tool every turn until MaxToolTurns is hit.
type stickyToolProvider struct {
	completes atomic.Int32
}

func (*stickyToolProvider) Name() string { return "sticky-tool" }

func (p *stickyToolProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	p.completes.Add(1)
	if len(req.Tools) == 0 {
		_ = emit(ProviderChunk{Delta: "done-without-tools"})
		return emit(ProviderChunk{Final: true})
	}
	return emit(ProviderChunk{ToolCalls: []tools.Call{{
		ID:        "tool_sticky",
		Name:      "workspace.list",
		Risk:      tools.RiskLow,
		Arguments: map[string]any{"path": ".", "max_depth": 1},
	}}})
}

func TestRunWithGoalLoopOpensMultipleSegmentsWhenBound(t *testing.T) {
	provider := &stickyToolProvider{}
	// Discard events so emit paths never block on an unread pipe.
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
				SuccessCriteria:   "all done",
				Status:            "active",
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
	// 3 segments × at least 1 Complete each (+ possible retryFinalAnswer with tools nil).
	if provider.completes.Load() < 3 {
		t.Fatalf("provider completes = %d, want >= 3 segments", provider.completes.Load())
	}
}

func TestGoalRunSegmentLimitExtendsToRemainingBudget(t *testing.T) {
	goal := methods.GoalDTO{
		UsedToolTurns:     71,
		MaxTotalToolTurns: 96,
		MaxSegmentsPerRun: 4,
		MaxToolTurnsSeg:   12,
	}
	if got := goalRunSegmentLimit(goal, 0); got != 4 {
		t.Fatalf("initial segment limit = %d, want configured minimum 4", got)
	}
	// If early segments consume less than their cap, the limit grows so the run
	// keeps moving instead of pausing for user confirmation.
	goal.UsedToolTurns = 73
	if got := goalRunSegmentLimit(goal, 4); got != 6 {
		t.Fatalf("expanded segment limit = %d, want 6", got)
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
	// Unbound: only one segment. Complete once for the tool turn, maybe once more for retry without tools.
	if provider.completes.Load() > 3 {
		t.Fatalf("unbound completes = %d, expected single-segment budget", provider.completes.Load())
	}
}

func TestMidRunGoalActivateExpandsSegments(t *testing.T) {
	// Seed unbound; after first segment we'd stop — but we pre-bind via setRunGoal
	// after simulating activate between segments by using BoundToThisRun from start
	// of second logic: seed bound mid-loop by mutating runGoals after first segment.
	// Here we verify get/set + maxSeg expansion path via seedRunGoalFromParams.
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
	// Activate path via applyGoalToolResult
	rt.applyGoalToolResult(params.RunID, "goal.write", methods.GoalToolExecuteResult{
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
	rt.applyGoalToolResult(params.RunID, "goal.complete", methods.GoalToolExecuteResult{
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

// TestBoundGoalMultiSegmentSingleRootFinalAndFinish locks A5 stream semantics:
// intermediate segments must not close the root message stream; exactly one
// root message_delta with final=true and exactly one root finish close the run.
func TestBoundGoalMultiSegmentSingleRootFinalAndFinish(t *testing.T) {
	provider := &stickyToolProvider{}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	// Multi-segment runs emit many notifications; keep the channel large so the
	// producer never blocks on a full buffer before the consumer drains finish.
	lines := make(chan []byte, 512)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	runID := "run_goal_stream_final"
	sendRequest(t, context.Background(), rt, jsonrpc.ID("reply_"+runID), methods.AgentReply, methods.ReplyParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:         "session_goal_stream_final",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "long goal multi-segment"},
		Options: methods.ReplyOptions{
			MaxToolTurns: 1,
			// Keep event volume low; A5 cares about message_delta.final + finish.
			EmitToolEvents: false,
			GoalID:         "goal_stream_final",
			GoalContext: &methods.GoalContext{
				GoalID:            "goal_stream_final",
				Objective:         "exercise multi-segment final ordering",
				SuccessCriteria:   "one root final",
				Status:            "active",
				MaxSegmentsPerRun: 3,
				MaxTotalToolTurns: 3,
				MaxToolTurnsSeg:   1,
			},
		},
	})
	waitForResponse(t, lines, jsonrpc.ID("reply_"+runID))
	runEvents := waitForEventsUntilFinishTimeout(t, lines, 10*time.Second)

	var messageFinals int
	var finishes int
	var sawMessageAfterFinal bool
	var finalSeen bool
	var streamID string
	for _, event := range runEvents {
		if event.Type == events.EventFinish {
			finishes++
			if event.RunID != runID || event.RootRunID != runID {
				t.Fatalf("finish not scoped to root run: %#v", event)
			}
			continue
		}
		if event.Type != events.EventMessageDelta {
			continue
		}
		if event.Stream == nil {
			t.Fatalf("message_delta missing stream: %#v", event)
		}
		if streamID == "" {
			streamID = event.Stream.StreamID
		} else if event.Stream.StreamID != streamID {
			t.Fatalf("root message stream id changed mid-run: %q -> %q", streamID, event.Stream.StreamID)
		}
		if finalSeen {
			sawMessageAfterFinal = true
		}
		if event.Stream.Final {
			messageFinals++
			finalSeen = true
		}
	}
	if provider.completes.Load() < 3 {
		t.Fatalf("provider completes = %d, want multi-segment (>=3)", provider.completes.Load())
	}
	if messageFinals != 1 {
		t.Fatalf("root message final count = %d, want exactly 1 across multi-segment run", messageFinals)
	}
	if finishes != 1 {
		t.Fatalf("root finish count = %d, want exactly 1", finishes)
	}
	if sawMessageAfterFinal {
		t.Fatal("message_delta emitted after root final=true")
	}
	if !finalSeen {
		t.Fatal("expected root stream final before finish")
	}
}

// TestUnboundRunAllowsProviderStreamFinal ensures A5 does not break normal chat:
// unbound single-segment runs still close the message stream from the provider path.
func TestUnboundRunAllowsProviderStreamFinal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = &textOnlyFinalProvider{text: "hello unbound"}

	runID := "run_unbound_final"
	sendRequest(t, context.Background(), rt, jsonrpc.ID("reply_"+runID), methods.AgentReply, methods.ReplyParams{
		RunID: runID,
		Session: methods.ReplySession{
			ID:         "session_unbound_final",
			WorkingDir: t.TempDir(),
		},
		Input:   methods.ReplyInput{Text: "hi"},
		Options: methods.ReplyOptions{MaxToolTurns: 1},
	})
	waitForResponse(t, lines, jsonrpc.ID("reply_"+runID))
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

// textOnlyFinalProvider emits a single text chunk closed with Final=true.
type textOnlyFinalProvider struct {
	text string
}

func (*textOnlyFinalProvider) Name() string { return "text-only-final" }

func (p *textOnlyFinalProvider) Complete(_ context.Context, _ ProviderRequest, emit func(ProviderChunk) error) error {
	_ = emit(ProviderChunk{Delta: p.text})
	return emit(ProviderChunk{Final: true})
}

// waitForEventsUntilFinishTimeout is like waitForEventsUntilFinish but allows
// multi-segment Goal runs that spend time on soft segment_end RPC timeouts.
func waitForEventsUntilFinishTimeout(t *testing.T, lines <-chan []byte, timeout time.Duration) []events.Envelope {
	t.Helper()
	var items []events.Envelope
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
			if probe.Method != methods.AgentEvent {
				continue
			}
			var note jsonrpc.Notification
			if err := json.Unmarshal(raw, &note); err != nil {
				t.Fatal(err)
			}
			var event events.Envelope
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
