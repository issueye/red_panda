package runtime

import (
	"testing"

	"redpanda/protocol/methods"
)

func TestEffectiveSegmentToolLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		goal methods.GoalDTO
		want int
	}{
		{
			name: "seg only",
			goal: methods.GoalDTO{MaxToolTurnsSeg: 8},
			want: 8,
		},
		{
			name: "total remaining tighter",
			goal: methods.GoalDTO{MaxToolTurnsSeg: 12, MaxTotalToolTurns: 20, UsedToolTurns: 15},
			want: 5,
		},
		{
			name: "remaining clamps to one",
			goal: methods.GoalDTO{MaxToolTurnsSeg: 12, MaxTotalToolTurns: 10, UsedToolTurns: 10},
			want: 1,
		},
		{
			name: "zero total ignores remaining math",
			goal: methods.GoalDTO{MaxToolTurnsSeg: 6, MaxTotalToolTurns: 0, UsedToolTurns: 100},
			want: 6,
		},
		{
			name: "zero seg uses remaining",
			goal: methods.GoalDTO{MaxToolTurnsSeg: 0, MaxTotalToolTurns: 9, UsedToolTurns: 3},
			want: 6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveSegmentToolLimit(tc.goal); got != tc.want {
				t.Fatalf("effectiveSegmentToolLimit = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestInitialGoalMaxSegments(t *testing.T) {
	t.Parallel()
	if got := initialGoalMaxSegments(nil); got != 1 {
		t.Fatalf("nil state = %d, want 1", got)
	}
	unbound := &runGoalState{BoundToThisRun: false, Goal: methods.GoalDTO{MaxSegmentsPerRun: 5}}
	if got := initialGoalMaxSegments(unbound); got != 1 {
		t.Fatalf("unbound = %d, want 1", got)
	}
	terminal := &runGoalState{BoundToThisRun: true, Terminal: true, Goal: methods.GoalDTO{MaxSegmentsPerRun: 5}}
	if got := initialGoalMaxSegments(terminal); got != 1 {
		t.Fatalf("terminal = %d, want 1", got)
	}
	bound := &runGoalState{BoundToThisRun: true, Goal: methods.GoalDTO{MaxSegmentsPerRun: 4}}
	if got := initialGoalMaxSegments(bound); got != 4 {
		t.Fatalf("bound = %d, want 4", got)
	}
}

func TestDecideGoalLoopContinue(t *testing.T) {
	t.Parallel()
	active := &runGoalState{
		BoundToThisRun: true,
		Goal: methods.GoalDTO{
			ID:                "g1",
			Status:            "active",
			MaxSegmentsPerRun: 3,
			MaxTotalToolTurns: 30,
			UsedToolTurns:     2,
		},
	}
	cases := []struct {
		name       string
		state      *runGoalState
		lastReason loopEndReason
		seg        int
		maxSeg     int
		wantCont   bool
		wantMax    int
	}{
		{
			name:       "hard cancel",
			state:      active,
			lastReason: loopEndCancelled,
			seg:        0,
			maxSeg:     3,
			wantCont:   false,
			wantMax:    3,
		},
		{
			name:       "hard failed",
			state:      active,
			lastReason: loopEndFailed,
			seg:        0,
			maxSeg:     3,
			wantCont:   false,
			wantMax:    3,
		},
		{
			name:       "hard budget",
			state:      active,
			lastReason: loopEndBudget,
			seg:        0,
			maxSeg:     3,
			wantCont:   false,
			wantMax:    3,
		},
		{
			name: "terminal goal",
			state: &runGoalState{
				BoundToThisRun: false,
				Terminal:       true,
				Goal:           methods.GoalDTO{ID: "g1", Status: "succeeded", MaxSegmentsPerRun: 3},
			},
			lastReason: loopEndNoTools,
			seg:        0,
			maxSeg:     3,
			wantCont:   false,
			wantMax:    3,
		},
		{
			name:       "unbound single segment",
			state:      nil,
			lastReason: loopEndMaxTurns,
			seg:        0,
			maxSeg:     1,
			wantCont:   false,
			wantMax:    1,
		},
		{
			name: "total budget exhausted",
			state: &runGoalState{
				BoundToThisRun: true,
				Goal: methods.GoalDTO{
					ID: "g1", Status: "active",
					MaxSegmentsPerRun: 4, MaxTotalToolTurns: 10, UsedToolTurns: 10,
				},
			},
			lastReason: loopEndMaxTurns,
			seg:        0,
			maxSeg:     4,
			wantCont:   false,
			wantMax:    4,
		},
		{
			name:       "soft no_tools continues",
			state:      active,
			lastReason: loopEndNoTools,
			seg:        0,
			maxSeg:     3,
			wantCont:   true,
			wantMax:    3,
		},
		{
			name:       "soft max_turns continues",
			state:      active,
			lastReason: loopEndMaxTurns,
			seg:        1,
			maxSeg:     3,
			wantCont:   true,
			wantMax:    3,
		},
		{
			name:       "maxSeg ceiling",
			state:      active,
			lastReason: loopEndMaxTurns,
			seg:        2,
			maxSeg:     3,
			wantCont:   false,
			wantMax:    3,
		},
		{
			name: "expand maxSeg when bound",
			state: &runGoalState{
				BoundToThisRun: true,
				Goal: methods.GoalDTO{
					ID: "g1", Status: "active",
					MaxSegmentsPerRun: 5, MaxTotalToolTurns: 50, UsedToolTurns: 1,
				},
			},
			lastReason: loopEndMaxTurns,
			seg:        0,
			maxSeg:     1, // started unbound then bound mid-run
			wantCont:   true,
			wantMax:    5,
		},
		{
			name: "was unbound after first segment",
			state: &runGoalState{
				BoundToThisRun: false,
				Goal:           methods.GoalDTO{ID: "g1", MaxSegmentsPerRun: 5},
			},
			lastReason: loopEndMaxTurns,
			seg:        0,
			maxSeg:     1,
			wantCont:   false,
			wantMax:    1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decideGoalLoopContinue(tc.state, tc.lastReason, tc.seg, tc.maxSeg)
			if got.Continue != tc.wantCont || got.MaxSeg != tc.wantMax {
				t.Fatalf("decision = {Continue:%v MaxSeg:%d}, want {Continue:%v MaxSeg:%d}",
					got.Continue, got.MaxSeg, tc.wantCont, tc.wantMax)
			}
		})
	}
}
