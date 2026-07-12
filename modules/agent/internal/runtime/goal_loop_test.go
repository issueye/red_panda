package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"redpanda/protocol/events"
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
