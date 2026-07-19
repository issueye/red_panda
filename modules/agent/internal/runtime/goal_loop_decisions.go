package runtime

import "redpanda/protocol/methods"

// effectiveSegmentToolLimit returns the per-segment MaxToolTurns cap for the
// next provider segment. Total budget remaining can only tighten MaxToolTurnsSeg;
// it never raises a client-provided segment cap above remaining turns.
func effectiveSegmentToolLimit(goal methods.GoalDTO) int {
	limit := goal.MaxToolTurnsSeg
	if goal.MaxTotalToolTurns > 0 {
		remaining := goal.MaxTotalToolTurns - goal.UsedToolTurns
		if remaining < 1 {
			remaining = 1
		}
		if limit <= 0 || remaining < limit {
			limit = remaining
		}
	}
	return limit
}

// initialGoalMaxSegments is the outer-loop segment ceiling at start of a run.
// Unbound / terminal goals stay at 1 (legacy single-segment reply path).
func initialGoalMaxSegments(state *runGoalState) int {
	if state != nil && state.BoundToThisRun && !state.Terminal {
		return goalRunSegmentLimit(state.Goal, 0)
	}
	return 1
}

// goalLoopDecision is the pure continue/stop choice after one provider segment.
type goalLoopDecision struct {
	Continue bool
	// MaxSeg is the updated outer-loop ceiling (may grow when a goal binds mid-run).
	MaxSeg int
}

// decideGoalLoopContinue encodes whether the outer Goal loop opens another
// provider segment. Rules match the historical inline tree in runWithGoalLoop:
// hard segment ends stop; terminal goals stop; unbound runs are single-segment;
// total tool budget exhaustion stops; only soft ends (no_tools / max_turns)
// may continue up to maxSeg.
func decideGoalLoopContinue(
	state *runGoalState,
	lastReason loopEndReason,
	seg int,
	maxSeg int,
) goalLoopDecision {
	out := goalLoopDecision{MaxSeg: maxSeg}

	if lastReason == loopEndCancelled || lastReason == loopEndFailed || lastReason == loopEndBudget {
		return out
	}
	if state != nil && state.Terminal {
		return out
	}

	// After the first segment, a mid-run goal.create may raise the run ceiling.
	if state != nil && state.BoundToThisRun {
		want := goalRunSegmentLimit(state.Goal, seg+1)
		if want > out.MaxSeg {
			out.MaxSeg = want
		}
	} else if state == nil || !state.BoundToThisRun {
		// Unbound: only a single segment.
		return out
	}

	if state != nil && state.Goal.MaxTotalToolTurns > 0 && state.Goal.UsedToolTurns >= state.Goal.MaxTotalToolTurns {
		return out
	}

	// Soft segment boundaries only.
	if lastReason != loopEndNoTools && lastReason != loopEndMaxTurns {
		return out
	}
	if seg+1 >= out.MaxSeg {
		return out
	}

	out.Continue = true
	return out
}
