package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/methods"
)

const (
	defaultGoalMaxSegmentsPerRun = 4
	carrySummarizedK             = 6
	carrySummarizedMaxRunes      = 500
	goalNoteDigestBodyRune       = 300
)

// runGoalState 是多分段控制器使用的单次运行 Goal 快照。
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
			Status:            firstNonEmpty(gc.Status, "active"),
			UsedToolTurns:     gc.UsedToolTurns,
			MaxTotalToolTurns: firstPositive(gc.MaxTotalToolTurns, 96),
			UsedSegments:      gc.UsedSegments,
			MaxSegmentsPerRun: firstPositive(gc.MaxSegmentsPerRun, defaultGoalMaxSegmentsPerRun),
			MaxToolTurnsSeg:   firstPositive(gc.MaxToolTurnsSeg, effectiveProviderToolTurns(params.Options), 12),
			UsedWallTimeSec:   gc.UsedWallTimeSec,
			MaxWallTimeSec:    gc.MaxWallTimeSec,
			Criteria:          gc.Criteria,
			Constraints:       gc.Constraints,
			Strategy:          gc.Strategy,
			CurrentActionID:   gc.CurrentActionID,
			CurrentAction:     gc.CurrentAction,
			Actions:           gc.Actions,
			LastObservation:   gc.LastObservation,
			LastAssessment:    gc.LastAssessment,
			LastDecision:      gc.LastDecision,
			Iteration:         gc.Iteration,
			MaxIterations:     gc.MaxIterations,
			StagnationCount:   gc.StagnationCount,
			MaxStagnation:     gc.MaxStagnation,
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
	r.runStates.SetGoal(runID, state)
}

func (r *Runtime) getRunGoal(runID string) *runGoalState {
	return r.runStates.Goal(runID)
}

func (r *Runtime) clearRunSnapshots(runID string) {
	r.runStates.ClearSnapshots(runID)
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
	// 若该运行已绑定且目标仍处于活动状态，则保留绑定标志。
	if prev != nil && prev.BoundToThisRun && g.Status == "active" {
		state.BoundToThisRun = true
	}
	// goal.create can activate a Goal in the middle of a regular run.
	if toolName == "goal.create" && g.Status == "active" {
		state.BoundToThisRun = true
		state.WasBoundToThisRun = true
	}
	if toolName == "goal.finish" || g.Status == "cancelled" {
		state.BoundToThisRun = false
		state.Terminal = true
	}
	r.setRunGoal(runID, state)

	// 刷新注入的 GoalContext，供后续提供方回合使用。
	if params := r.lookupActiveReplyParams(runID); params != nil {
		params.Options.GoalContext = goalDTOToContext(g)
		params.Options.GoalID = g.ID
	}
}

// lookupActiveReplyParams 是空操作钩子占位符；分段循环会通过 goalContextForRun
// 从 RunStateStore 刷新 GoalContext。
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

// deferGoalStreamFinal 在多个 Goal 分段间保持同一根消息流打开。
// 外层循环停止后由根运行器发送唯一的最终标记。
func (r *Runtime) deferGoalStreamFinal(runID string) bool {
	state := r.getRunGoal(runID)
	return state != nil && state.Goal.ID != ""
}

func goalDTOToContext(g methods.GoalDTO) *methods.GoalContext {
	return &methods.GoalContext{
		GoalID:            g.ID,
		Title:             g.Title,
		Objective:         g.Objective,
		Status:            g.Status,
		UsedToolTurns:     g.UsedToolTurns,
		MaxTotalToolTurns: g.MaxTotalToolTurns,
		UsedSegments:      g.UsedSegments,
		MaxSegmentsPerRun: g.MaxSegmentsPerRun,
		MaxToolTurnsSeg:   g.MaxToolTurnsSeg,
		UsedWallTimeSec:   g.UsedWallTimeSec,
		MaxWallTimeSec:    g.MaxWallTimeSec,
		Context:           formatGoalContextFromDTO(g),
		Criteria:          g.Criteria,
		Constraints:       g.Constraints,
		Strategy:          g.Strategy,
		CurrentActionID:   g.CurrentActionID,
		CurrentAction:     g.CurrentAction,
		Actions:           g.Actions,
		LastObservation:   g.LastObservation,
		LastAssessment:    g.LastAssessment,
		LastDecision:      g.LastDecision,
		Iteration:         g.Iteration,
		MaxIterations:     g.MaxIterations,
		StagnationCount:   g.StagnationCount,
		MaxStagnation:     g.MaxStagnation,
	}
}

func formatGoalContextFromDTO(g methods.GoalDTO) string {
	var b strings.Builder
	b.WriteString("Goal controller state (plan -> act -> observe -> assess -> decide):\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", g.ID))
	b.WriteString(fmt.Sprintf("- status: %s\n", g.Status))
	b.WriteString(fmt.Sprintf("- objective: %s\n", g.Objective))
	if len(g.Criteria) > 0 {
		b.WriteString("- criteria:\n")
		for _, criterion := range g.Criteria {
			status := criterion.Status
			if status == "" {
				status = "unknown"
			}
			b.WriteString(fmt.Sprintf("  - [%s] %s\n", status, criterion.Description))
		}
	}
	if g.Strategy != "" {
		b.WriteString(fmt.Sprintf("- strategy: %s\n", g.Strategy))
	}
	if g.CurrentAction != "" {
		b.WriteString(fmt.Sprintf("- current_action: %s (%s)\n", g.CurrentAction, g.CurrentActionID))
	}
	if g.LastObservation != "" {
		b.WriteString(fmt.Sprintf("- last_observation: %s\n", g.LastObservation))
	}
	if g.LastAssessment != nil {
		b.WriteString(fmt.Sprintf("- last_assessment: %s — %s\n", g.LastAssessment.Verdict, g.LastAssessment.Summary))
		if g.LastAssessment.Gap != "" {
			b.WriteString(fmt.Sprintf("- remaining_gap: %s\n", g.LastAssessment.Gap))
		}
	}
	b.WriteString(fmt.Sprintf("- control: iteration %d/%d, stagnation %d/%d\n", g.Iteration, g.MaxIterations, g.StagnationCount, g.MaxStagnation))
	b.WriteString(fmt.Sprintf("- budget: %d/%d root tool turns\n", g.UsedToolTurns, g.MaxTotalToolTurns))
	b.WriteString("- tools: goal.plan chooses actions; goal.observe records facts; goal.assess decides against criteria; goal.finish terminalizes only after satisfied\n")
	return b.String()
}

// runWithGoalLoop 为根回复执行一个或多个提供方分段。
// 未绑定运行使用 maxSeg=1（旧版单分段）；绑定 Goal 的运行会在 no_tools 或 max_turns 时
// 打开更多分段，直至达到本次 run 上限、预算耗尽或调用 goal.finish。
func (r *Runtime) runWithGoalLoop(
	ctx context.Context,
	params methods.ReplyParams,
	input string,
	history []provider.ToolExchange,
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
	maxSeg := initialGoalMaxSegments(state)

	seedHistory := append([]provider.ToolExchange(nil), history...)
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

		// 每个分段均刷新上下文。
		if ctxTodos := r.todoContextForRun(params.RunID); ctxTodos != nil {
			params.Options.TodoContext = ctxTodos
		}
		if ctxGoal := r.goalContextForRun(params.RunID); ctxGoal != nil {
			// 将置顶和最近的暂存区笔记合并至目标上下文，使分段 N+1 无需显式
			// 调用 context.read 即可看到分段 N 的关键发现。尽力而为：失败只记录日志，不会终止运行。
			if notes := r.goalNotesDigest(params.RunID, params.Session.ID, ctxGoal.GoalID); notes != "" {
				ctxGoal.Context = strings.TrimSpace(ctxGoal.Context) + "\n\n" + notes
			}
			params.Options.GoalContext = ctxGoal
		}

		// 每个分段以 Goal 预算为准；剩余工具回合只能进一步收紧，不能提高客户端选项。
		if state := r.getRunGoal(params.RunID); state != nil {
			if limit := effectiveSegmentToolLimit(state.Goal); limit > 0 {
				params.Options.MaxToolTurns = limit
			}
		}
		last = r.runProviderLoopSegment(loopCtx, params, segmentInput, seedHistory, messageID, streamID, streamSeq)

		// 绑定目标时向 Gateway 上报分段预算。
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

		decision := decideGoalLoopContinue(state, last.Reason, seg, maxSeg)
		maxSeg = decision.MaxSeg
		if !decision.Continue {
			return last
		}

		seedHistory = carrySummarizedHistory(last.History, carrySummarizedK, carrySummarizedMaxRunes)
		segmentInput = goalContinuationPrompt(input, state, seg+1, last.Reason)
	}
	return last
}

// goalRunSegmentLimit is a hard per-run transport cap. Goal progress is measured
// by assessments and actions, never by silently expanding provider segments.
func goalRunSegmentLimit(goal methods.GoalDTO, completedThisRun int) int {
	return firstPositive(goal.MaxSegmentsPerRun, defaultGoalMaxSegmentsPerRun)
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
	// 限制 RPC 时间，避免缺少 Gateway 时外层多分段循环停滞。
	// 本地 Gateway 应在此期限内响应；未配置 Gateway 的测试也能快速失败。
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
		// 仍推进本地计数器，使离线和测试场景下外层循环仍可进行预算决策。
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
	b.WriteString("Do not claim the Goal is finished without a persisted satisfied assessment. ")
	b.WriteString("Continue the active action, record the real result with goal.observe, then call goal.assess against every criterion. ")
	b.WriteString("If the action did not reduce the gap, revise the strategy instead of repeating it.\n")
	if state != nil {
		if state.Goal.LastAssessment != nil {
			b.WriteString("Last assessment: ")
			b.WriteString(state.Goal.LastAssessment.Verdict)
			b.WriteString(" - ")
			b.WriteString(state.Goal.LastAssessment.Summary)
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

// goalNotesDigest 从 Gateway 读取目标的置顶和最近暂存区笔记，并生成紧凑摘要。
// 它将共享发现自动注入每个新目标分段；无笔记或 Gateway 不可用时返回空字符串，
// 采用尽力而为策略，绝不阻塞目标循环。
func (r *Runtime) goalNotesDigest(runID string, sessionID string, goalID string) string {
	notes := r.fetchGoalNotes(runID, sessionID, goalID, 10)
	return renderNotesDigest("Shared goal notes", notes)
}

// goalNotesBrief 为专业子代理生成目标、置顶和最近笔记的摘要。
// 摘要包含 goal_id，使子代理可自行调用 context.read 或 context.search。
func (r *Runtime) goalNotesBrief(runID string, sessionID string, goalID string, objective string) string {
	header := fmt.Sprintf("Parent goal %q (use context.read with goal_id=%s to read full notes):\nObjective: %s\n", goalID, goalID, strings.TrimSpace(objective))
	notes := r.fetchGoalNotes(runID, sessionID, goalID, 8)
	if len(notes) == 0 {
		return header + "(no shared notes yet)"
	}
	return header + renderNotesDigest("", notes)
}

// fetchGoalNotes 通过 Gateway RPC 调用 context.read 工具。该操作尽力而为：
// 任意错误都会返回 nil，供调用方平滑降级。
// sessionID 必填：Gateway 会拒绝空 session_id，缺少它会使每个分段和专业子代理交接的自动注入静默失效。
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
