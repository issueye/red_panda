package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redpanda/protocol/methods"
)

const (
	defaultGoalMaxSegmentsPerRun = 4
	carrySummarizedK             = 6
	carrySummarizedMaxRunes      = 500
	goalNoteDigestBodyRune       = 300
)

// runGoalState is the per-run Goal snapshot used by the multi-segment controller.
type runGoalState struct {
	Goal              methods.GoalDTO
	BoundToThisRun    bool
	WasBoundToThisRun bool
	Terminal          bool
}

func (r *Runtime) seedRunGoalFromParams(params methods.ReplyParams) {
	if params.Options.GoalContext == nil && strings.TrimSpace(params.Options.GoalID) == "" {
		return
	}
	state := &runGoalState{BoundToThisRun: true, WasBoundToThisRun: true}
	if params.Options.GoalContext != nil {
		gc := params.Options.GoalContext
		state.Goal = methods.GoalDTO{
			ID:                gc.GoalID,
			SessionID:         params.Session.ID,
			Title:             gc.Title,
			Objective:         gc.Objective,
			SuccessCriteria:   gc.SuccessCriteria,
			Status:            firstNonEmpty(gc.Status, "active"),
			PipelinePhase:     gc.PipelinePhase,
			AnalysisSummary:   gc.AnalysisSummary,
			CheckpointSummary: gc.CheckpointSummary,
			ProgressNote:      gc.ProgressNote,
			UsedToolTurns:     gc.UsedToolTurns,
			MaxTotalToolTurns: firstPositive(gc.MaxTotalToolTurns, 96),
			UsedSegments:      gc.UsedSegments,
			MaxSegmentsPerRun: firstPositive(gc.MaxSegmentsPerRun, defaultGoalMaxSegmentsPerRun),
			MaxToolTurnsSeg:   firstPositive(gc.MaxToolTurnsSeg, effectiveProviderToolTurns(params.Options), 12),
			UsedWallTimeSec:   gc.UsedWallTimeSec,
			MaxWallTimeSec:    gc.MaxWallTimeSec,
		}
		if state.Goal.ID == "" {
			state.Goal.ID = params.Options.GoalID
		}
	} else {
		state.Goal = methods.GoalDTO{
			ID:                params.Options.GoalID,
			SessionID:         params.Session.ID,
			Status:            "active",
			MaxSegmentsPerRun: defaultGoalMaxSegmentsPerRun,
			MaxTotalToolTurns: 96,
			MaxToolTurnsSeg:   firstPositive(effectiveProviderToolTurns(params.Options), 12),
		}
	}
	r.setRunGoal(params.RunID, state)
}

func (r *Runtime) setRunGoal(runID string, state *runGoalState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runGoals == nil {
		r.runGoals = map[string]*runGoalState{}
	}
	if state == nil {
		delete(r.runGoals, runID)
		return
	}
	cp := *state
	r.runGoals[runID] = &cp
}

func (r *Runtime) getRunGoal(runID string) *runGoalState {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.runGoals[runID]
	if state == nil {
		return nil
	}
	cp := *state
	return &cp
}

func (r *Runtime) clearRunGoal(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runGoals, runID)
}

func (r *Runtime) clearRunSnapshots(runID string) {
	r.clearRunTodos(runID)
	r.clearRunGoal(runID)
}

func (r *Runtime) applyGoalToolResult(runID string, toolName string, result methods.GoalToolExecuteResult) {
	if result.Goal == nil {
		return
	}
	g := *result.Goal
	terminal := g.Status == "succeeded" || g.Status == "failed" || g.Status == "cancelled"
	bound := g.Status == "active"
	prev := r.getRunGoal(runID)
	state := &runGoalState{
		Goal:              g,
		BoundToThisRun:    bound,
		WasBoundToThisRun: bound,
		Terminal:          terminal,
	}
	if prev != nil && prev.WasBoundToThisRun {
		state.WasBoundToThisRun = true
	}
	// Keep bound flag if we already bound this run and goal remains active.
	if prev != nil && prev.BoundToThisRun && g.Status == "active" {
		state.BoundToThisRun = true
	}
	// goal.write/update activate mid-run.
	if (toolName == "goal.write" || toolName == "goal.update") && g.Status == "active" {
		state.BoundToThisRun = true
		state.WasBoundToThisRun = true
	}
	if toolName == "goal.complete" || g.Status == "cancelled" {
		state.BoundToThisRun = false
		state.Terminal = true
	}
	r.setRunGoal(runID, state)

	// Refresh injected GoalContext for subsequent provider turns.
	if params := r.lookupActiveReplyParams(runID); params != nil {
		params.Options.GoalContext = goalDTOToContext(g)
		params.Options.GoalID = g.ID
	}
}

// lookupActiveReplyParams is a no-op hook placeholder — GoalContext is refreshed
// from runGoals inside the segment loop via goalContextForRun.
func (r *Runtime) lookupActiveReplyParams(runID string) *methods.ReplyParams {
	return nil
}

func (r *Runtime) goalContextForRun(runID string) *methods.GoalContext {
	state := r.getRunGoal(runID)
	if state == nil || state.Goal.ID == "" {
		return nil
	}
	return goalDTOToContext(state.Goal)
}

// deferGoalStreamFinal keeps one root message stream open across Goal segments.
// The root runner emits the single final marker after the outer loop stops.
func (r *Runtime) deferGoalStreamFinal(runID string) bool {
	state := r.getRunGoal(runID)
	return state != nil && state.Goal.ID != ""
}

func goalDTOToContext(g methods.GoalDTO) *methods.GoalContext {
	return &methods.GoalContext{
		GoalID:            g.ID,
		Title:             g.Title,
		Objective:         g.Objective,
		SuccessCriteria:   g.SuccessCriteria,
		Status:            g.Status,
		PipelinePhase:     g.PipelinePhase,
		AnalysisSummary:   g.AnalysisSummary,
		CheckpointSummary: g.CheckpointSummary,
		ProgressNote:      g.ProgressNote,
		UsedToolTurns:     g.UsedToolTurns,
		MaxTotalToolTurns: g.MaxTotalToolTurns,
		UsedSegments:      g.UsedSegments,
		MaxSegmentsPerRun: g.MaxSegmentsPerRun,
		MaxToolTurnsSeg:   g.MaxToolTurnsSeg,
		UsedWallTimeSec:   g.UsedWallTimeSec,
		MaxWallTimeSec:    g.MaxWallTimeSec,
		Context:           formatGoalContextFromDTO(g),
	}
}

func formatGoalContextFromDTO(g methods.GoalDTO) string {
	var b strings.Builder
	b.WriteString("Active goal (update via goal.checkpoint / goal.complete; steps via todo.write):\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", g.ID))
	b.WriteString(fmt.Sprintf("- status: %s phase: %s\n", g.Status, g.PipelinePhase))
	b.WriteString(fmt.Sprintf("- objective: %s\n", g.Objective))
	if g.SuccessCriteria != "" {
		b.WriteString(fmt.Sprintf("- success_criteria: %s\n", g.SuccessCriteria))
	}
	if g.CheckpointSummary != "" {
		b.WriteString(fmt.Sprintf("- checkpoint: %s\n", g.CheckpointSummary))
	}
	b.WriteString(fmt.Sprintf("- budget: %d/%d root tool turns, segments used %d (max %d/run)\n",
		g.UsedToolTurns, g.MaxTotalToolTurns, g.UsedSegments, g.MaxSegmentsPerRun))
	return b.String()
}

// runWithGoalLoop runs one or more provider segments for a root reply.
// Unbound runs use maxSeg=1 (legacy single segment). Bound Goal runs may open
// additional segments on no_tools/max_turns until budgets or goal.complete.
func (r *Runtime) runWithGoalLoop(
	ctx context.Context,
	params methods.ReplyParams,
	input string,
	history []ToolExchange,
	messageID string,
	streamID string,
	streamSeq *uint64,
) providerSegmentResult {
	r.seedRunGoalFromParams(params)

	state := r.getRunGoal(params.RunID)
	loopStarted := time.Now()
	loopCtx := ctx
	var deadlineCancel context.CancelFunc
	deadlineConfigured := false
	defer func() {
		if deadlineCancel != nil {
			deadlineCancel()
		}
	}()
	configureDeadline := func(current *runGoalState, segmentIndex int) bool {
		if deadlineConfigured || current == nil || !current.BoundToThisRun || current.Goal.MaxWallTimeSec <= 0 {
			return true
		}
		remaining := time.Duration(current.Goal.MaxWallTimeSec-current.Goal.UsedWallTimeSec)*time.Second - time.Since(loopStarted)
		if remaining <= 0 {
			r.reportGoalBudgetExhausted(params, current.Goal.ID, segmentIndex, 0)
			return false
		}
		loopCtx, deadlineCancel = context.WithTimeout(ctx, remaining)
		deadlineConfigured = true
		return true
	}
	if !configureDeadline(state, 0) {
		return providerSegmentResult{Reason: loopEndBudget}
	}
	maxSeg := 1
	if state != nil && state.BoundToThisRun && !state.Terminal {
		maxSeg = goalRunSegmentLimit(state.Goal, 0)
	}

	seedHistory := append([]ToolExchange(nil), history...)
	var last providerSegmentResult
	segmentInput := input

	for seg := 0; seg < maxSeg; seg++ {
		if loopCtx.Err() != nil {
			if loopCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
				if current := r.getRunGoal(params.RunID); current != nil {
					r.reportGoalBudgetExhausted(params, current.Goal.ID, seg, last.ToolTurns)
				}
				return providerSegmentResult{Reason: loopEndBudget, ToolTurns: last.ToolTurns, History: last.History}
			}
			return providerSegmentResult{Reason: loopEndCancelled, ToolTurns: last.ToolTurns, History: last.History}
		}

		// Refresh contexts each segment.
		if ctxTodos := r.todoContextForRun(params.RunID); ctxTodos != nil {
			params.Options.TodoContext = ctxTodos
		}
		if ctxGoal := r.goalContextForRun(params.RunID); ctxGoal != nil {
			// Splice pinned/recent scratchpad notes into the goal context so
			// segment N+1 sees segment N's key findings without an explicit
			// context.read call. Best-effort: failures are logged, not fatal.
			if notes := r.goalNotesDigest(params.RunID, params.Session.ID, ctxGoal.GoalID); notes != "" {
				ctxGoal.Context = strings.TrimSpace(ctxGoal.Context) + "\n\n" + notes
			}
			params.Options.GoalContext = ctxGoal
		}

		if state := r.getRunGoal(params.RunID); state != nil && state.Goal.MaxToolTurnsSeg > 0 {
			params.Options.MaxToolTurns = state.Goal.MaxToolTurnsSeg
		}
		last = r.runProviderLoopSegment(loopCtx, params, segmentInput, seedHistory, messageID, streamID, streamSeq)

		// Report segment budget to Gateway when a goal is bound.
		state = r.getRunGoal(params.RunID)
		if state != nil && state.WasBoundToThisRun && state.Goal.ID != "" && last.ToolTurns > 0 {
			r.reportSegmentEnd(loopCtx, params, state.Goal.ID, seg, last.ToolTurns)
			state = r.getRunGoal(params.RunID)
		}
		if loopCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			if state != nil {
				r.reportGoalBudgetExhausted(params, state.Goal.ID, seg, last.ToolTurns)
			}
			return providerSegmentResult{Reason: loopEndBudget, ToolTurns: last.ToolTurns, History: last.History}
		}
		if !configureDeadline(state, seg) {
			return providerSegmentResult{Reason: loopEndBudget, ToolTurns: last.ToolTurns, History: last.History}
		}

		if last.Reason == loopEndCancelled || last.Reason == loopEndFailed || last.Reason == loopEndBudget {
			return last
		}
		if state != nil && state.Terminal {
			return last
		}

		// Mid-run activate may expand maxSeg after first segment.
		if state != nil && state.BoundToThisRun {
			want := goalRunSegmentLimit(state.Goal, seg+1)
			if want > maxSeg {
				maxSeg = want
			}
		} else if state == nil || !state.BoundToThisRun {
			// Unbound: single segment only.
			return last
		}

		// Total budget exhausted (Gateway may have terminalized).
		if state != nil && state.Goal.MaxTotalToolTurns > 0 && state.Goal.UsedToolTurns >= state.Goal.MaxTotalToolTurns {
			return last
		}

		// Continue outer loop only on soft segment boundaries.
		if last.Reason != loopEndNoTools && last.Reason != loopEndMaxTurns {
			return last
		}
		if seg+1 >= maxSeg {
			return last
		}

		seedHistory = carrySummarizedHistory(last.History, carrySummarizedK, carrySummarizedMaxRunes)
		segmentInput = goalContinuationPrompt(input, state, seg+1, last.Reason)
	}
	return last
}

// goalRunSegmentLimit keeps a bound Goal moving without asking the user to
// manually start another run at an arbitrary segment boundary. The persisted
// per-run value remains the minimum chunk size; remaining budget can extend the
// same run, which is still bounded by total tool turns and wall time.
func goalRunSegmentLimit(goal methods.GoalDTO, completedThisRun int) int {
	limit := firstPositive(goal.MaxSegmentsPerRun, defaultGoalMaxSegmentsPerRun)
	remaining := goal.MaxTotalToolTurns - goal.UsedToolTurns
	if goal.MaxTotalToolTurns <= 0 || remaining <= 0 {
		return limit
	}
	perSegment := firstPositive(goal.MaxToolTurnsSeg, 1)
	needed := (remaining + perSegment - 1) / perSegment
	want := completedThisRun + needed
	if want > limit {
		return want
	}
	return limit
}

func (r *Runtime) reportGoalBudgetExhausted(params methods.ReplyParams, goalID string, segmentIndex, deltaTurns int) {
	callCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.executeGoalTool(callCtx, methods.GoalToolExecuteParams{
		RunID: params.RunID, SessionID: params.Session.ID,
		ToolCallID: fmt.Sprintf("goal_budget_%s_%d", params.RunID, segmentIndex),
		ToolName:   "segment_end",
		Arguments: map[string]any{
			"goal_id": goalID, "segment_index": segmentIndex,
			"delta_tool_turns": deltaTurns, "budget_exhausted": true,
		},
	})
	if err == nil {
		r.applyGoalToolResult(params.RunID, "segment_end", result)
	}
}

func (r *Runtime) reportSegmentEnd(ctx context.Context, params methods.ReplyParams, goalID string, segmentIndex, deltaTurns int) {
	if deltaTurns <= 0 || goalID == "" {
		return
	}
	// Bound RPC so a missing gateway cannot stall the outer multi-segment loop.
	// Local gateway should answer well under this; tests without a gateway also fail fast.
	callCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	result, err := r.executeGoalTool(callCtx, methods.GoalToolExecuteParams{
		RunID:      params.RunID,
		SessionID:  params.Session.ID,
		ToolCallID: fmt.Sprintf("segment_end_%s_%d", params.RunID, segmentIndex),
		ToolName:   "segment_end",
		Arguments: map[string]any{
			"goal_id":          goalID,
			"segment_index":    segmentIndex,
			"delta_tool_turns": deltaTurns,
		},
	})
	if err != nil {
		fmt.Fprintf(r.log, "segment_end: %v\n", err)
		// Still advance local counters so outer-loop budget decisions remain possible offline/tests.
		if state := r.getRunGoal(params.RunID); state != nil {
			state.Goal.UsedToolTurns += deltaTurns
			state.Goal.UsedSegments++
			if state.Goal.MaxTotalToolTurns > 0 && state.Goal.UsedToolTurns >= state.Goal.MaxTotalToolTurns {
				state.Goal.Status = "failed"
				state.Goal.FailReason = "budget_exhausted"
				state.Terminal = true
				state.BoundToThisRun = false
			}
			r.setRunGoal(params.RunID, state)
		}
		return
	}
	r.applyGoalToolResult(params.RunID, "segment_end", result)
}

func goalContinuationPrompt(original string, state *runGoalState, nextSeg int, reason loopEndReason) string {
	var b strings.Builder
	b.WriteString(original)
	b.WriteString("\n\n[System] Goal segment boundary reached (")
	b.WriteString(string(reason))
	b.WriteString(fmt.Sprintf("). Continuing segment %d for the active goal. ", nextSeg+1))
	b.WriteString("Do not claim the goal is finished without goal.complete after evaluation. ")
	b.WriteString("Continue execute/verify for the current todo step, or advance todos, then checkpoint.\n")
	if state != nil {
		if state.Goal.CheckpointSummary != "" {
			b.WriteString("Last checkpoint: ")
			b.WriteString(state.Goal.CheckpointSummary)
			b.WriteString("\n")
		}
		if state.Goal.Objective != "" {
			b.WriteString("Objective: ")
			b.WriteString(state.Goal.Objective)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// goalNotesDigest reads pinned + recent scratchpad notes for a goal from the
// Gateway and renders a compact digest. Used to auto-inject shared findings into
// each new goal segment. Returns "" when there are no notes or the gateway is
// unavailable (best-effort, never blocks the goal loop).
func (r *Runtime) goalNotesDigest(runID string, sessionID string, goalID string) string {
	notes := r.fetchGoalNotes(runID, sessionID, goalID, 10)
	return renderNotesDigest("Shared goal notes", notes)
}

// goalNotesBrief renders the goal objective + pinned/recent notes for a specialist
// child. It includes the goal_id so the child can call context.read/search itself.
func (r *Runtime) goalNotesBrief(runID string, sessionID string, goalID string, objective string) string {
	header := fmt.Sprintf("Parent goal %q (use context.read with goal_id=%s to read full notes):\nObjective: %s\n", goalID, goalID, strings.TrimSpace(objective))
	notes := r.fetchGoalNotes(runID, sessionID, goalID, 8)
	if len(notes) == 0 {
		return header + "(no shared notes yet)"
	}
	return header + renderNotesDigest("", notes)
}

// fetchGoalNotes calls the context.read tool via the gateway RPC. Best-effort:
// on any error returns nil so callers degrade gracefully.
// sessionID is required: Gateway rejects empty session_id, and without it
// auto-inject would silently no-op every segment/specialist handoff.
func (r *Runtime) fetchGoalNotes(runID string, sessionID string, goalID string, limit int) []methods.GoalNoteDTO {
	if strings.TrimSpace(goalID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	callCtx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	result, err := r.executeContextTool(callCtx, methods.ContextToolExecuteParams{
		RunID:      runID,
		SessionID:  sessionID,
		ToolCallID: fmt.Sprintf("notes_inject_%s_%d", runID, time.Now().UnixNano()),
		ToolName:   "context.read",
		Arguments:  map[string]any{"goal_id": goalID, "limit": limit},
	})
	if err != nil {
		return nil
	}
	return result.Notes
}

func renderNotesDigest(heading string, notes []methods.GoalNoteDTO) string {
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	if heading != "" {
		b.WriteString(heading)
		b.WriteString(":\n")
	}
	for _, n := range notes {
		pin := ""
		if n.Pinned != 0 {
			pin = "[pinned] "
		}
		title := strings.TrimSpace(n.Title)
		if title == "" {
			title = "(untitled)"
		}
		body := strings.TrimSpace(n.Body)
		if len(body) > goalNoteDigestBodyRune {
			body = string([]rune(body)[:goalNoteDigestBodyRune]) + "…"
		}
		fmt.Fprintf(&b, "- %s[%s] %s", pin, n.Kind, title)
		if body != "" {
			b.WriteString(": ")
			b.WriteString(body)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
