package service

// Goal lifecycle, binding, pause/repair, and tool dispatch entrypoints.
// Feedback-controller mutations live in goal_controller.go (docs/41 W5-3).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

const (
	goalMaxSessionRows   = 20
	goalMaxObjectiveRune = 4000
	goalMaxCriteriaRune  = 4000
	goalMaxTitleRune     = 200
	goalContextLimit     = 2000
)

type GoalService struct {
	repos repository.Set
}

func NewGoalService(repos repository.Set) GoalService {
	return GoalService{repos: repos}
}

func (s GoalService) ListBySession(sessionID string) ([]methods.GoalDTO, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return nil, fmt.Errorf("session not found")
	}
	rows, err := s.repos.Goals.ListBySession(sessionID, 50)
	if err != nil {
		return nil, err
	}
	out := make([]methods.GoalDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.goalDTO(row))
	}
	return out, nil
}

func (s GoalService) Get(sessionID, goalID string) (methods.GoalDTO, error) {
	row, err := s.repos.Goals.Get(goalID)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	if sessionID != "" && row.SessionID != sessionID {
		return methods.GoalDTO{}, fmt.Errorf("goal not found")
	}
	return s.goalDTO(row), nil
}

func (s GoalService) CancelGoal(sessionID, goalID string) (methods.GoalDTO, error) {
	row, err := s.repos.Goals.Get(goalID)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	if row.SessionID != sessionID {
		return methods.GoalDTO{}, fmt.Errorf("goal not found")
	}
	switch row.Status {
	case "succeeded", "failed", "cancelled":
		// Idempotent: terminal Goals are left unchanged.
		return s.goalDTO(row), nil
	}
	lastRunID := row.ActiveRunID
	if lastRunID == "" {
		lastRunID = row.LastRunID
	}
	now := time.Now().UTC()
	changed, err := s.repos.Goals.TransitionStatus(row.ID, sessionID,
		[]string{"pending", "active", "paused"}, "", map[string]any{
			"status":        "cancelled",
			"active_run_id": "",
			"last_run_id":   lastRunID,
			"finished_at":   &now,
		})
	if err != nil {
		return methods.GoalDTO{}, err
	}
	updated, err := s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	if !changed {
		// Concurrent complete/fail/cancel won the CAS; surface the winner without
		// error so callers treat cancel as best-effort terminalization.
		return s.goalDTO(updated), nil
	}
	_ = s.appendGoalEvent(updated, lastRunID, "cancelled", "Goal cancelled", map[string]any{"previous_status": row.Status})
	return s.goalDTO(updated), nil
}

func (s GoalService) ExecuteRuntimeTool(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	if err := validateRuntimeToolMeta(s.repos, params.RunID, params.SessionID, params.ToolCallID, true); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	name := strings.TrimSpace(params.ToolName)
	switch name {
	case "goal.create":
		return s.executeCreateV2(params)
	case "goal.plan":
		return s.executePlanV2(params)
	case "goal.observe":
		return s.executeObserveV2(params)
	case "goal.assess":
		return s.executeAssessV2(params)
	case "goal.finish":
		return s.executeFinishV2(params)
	case "goal.list":
		return s.executeList(params)
	case "segment_end":
		return s.executeSegmentEnd(params)
	default:
		return methods.GoalToolExecuteResult{}, fmt.Errorf("unsupported goal tool %q", name)
	}
}

func (s GoalService) FormatGoalContext(sessionID string, goalID string) (*methods.GoalContext, error) {
	var row model.Goal
	var err error
	if goalID != "" {
		row, err = s.repos.Goals.Get(goalID)
	} else {
		row, err = s.repos.Goals.GetActiveBySession(sessionID)
	}
	if err != nil {
		return nil, nil
	}
	if row.SessionID != sessionID && sessionID != "" {
		return nil, nil
	}
	dto := s.goalDTO(row)
	ctx := formatGoalContextV2(dto)
	if utf8.RuneCountInString(ctx) > goalContextLimit {
		runes := []rune(ctx)
		ctx = string(runes[:goalContextLimit]) + "…"
	}
	return &methods.GoalContext{
		GoalID:            row.ID,
		Title:             row.Title,
		Objective:         row.Objective,
		Status:            row.Status,
		UsedToolTurns:     row.UsedToolTurns,
		MaxTotalToolTurns: row.MaxTotalToolTurns,
		UsedSegments:      row.UsedSegments,
		MaxSegmentsPerRun: row.MaxSegmentsPerRun,
		MaxToolTurnsSeg:   row.MaxToolTurnsPerSegment,
		UsedWallTimeSec:   row.UsedWallTimeSec,
		MaxWallTimeSec:    row.MaxWallTimeSec,
		Context:           ctx,
		Criteria:          dto.Criteria,
		Constraints:       dto.Constraints,
		Strategy:          dto.Strategy,
		CurrentActionID:   dto.CurrentActionID,
		CurrentAction:     dto.CurrentAction,
		Actions:           dto.Actions,
		LastObservation:   dto.LastObservation,
		LastAssessment:    dto.LastAssessment,
		LastDecision:      dto.LastDecision,
		Iteration:         dto.Iteration,
		MaxIterations:     dto.MaxIterations,
		StagnationCount:   dto.StagnationCount,
		MaxStagnation:     dto.MaxStagnation,
	}, nil
}

// CreateUserInitiated inserts a pending Goal from a Desktop slash command / HTTP start.
// The Goal is not active until BindToRun (run.start with goal_id / create_goal).
func (s GoalService) CreateUserInitiated(sessionID, objective, title, successCriteria string) (methods.GoalDTO, error) {
	sessionID = strings.TrimSpace(sessionID)
	objective = strings.TrimSpace(objective)
	if sessionID == "" {
		return methods.GoalDTO{}, fmt.Errorf("session id is required")
	}
	if objective == "" {
		return methods.GoalDTO{}, fmt.Errorf("objective is required")
	}
	if utf8.RuneCountInString(objective) > goalMaxObjectiveRune {
		return methods.GoalDTO{}, fmt.Errorf("objective too long")
	}
	if _, err := s.repos.Sessions.Get(sessionID); err != nil {
		return methods.GoalDTO{}, fmt.Errorf("session not found")
	}
	if err := s.RepairStaleActive(sessionID); err != nil {
		return methods.GoalDTO{}, err
	}
	if _, err := s.repos.Goals.GetActiveBySession(sessionID); err == nil {
		return methods.GoalDTO{}, fmt.Errorf("session already has an active goal; cancel or wait before starting a new one")
	}
	n, err := s.repos.Goals.CountBySession(sessionID)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	if n >= goalMaxSessionRows {
		return methods.GoalDTO{}, fmt.Errorf("session goal limit %d reached", goalMaxSessionRows)
	}
	criteria := strings.TrimSpace(successCriteria)
	if criteria == "" {
		criteria = "完成用户所述目标，并通过合理验证（测试/检查/可演示结果）。"
	}
	if utf8.RuneCountInString(criteria) > goalMaxCriteriaRune {
		return methods.GoalDTO{}, fmt.Errorf("success_criteria too long")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = truncateRunes(objective, 40)
	}
	if utf8.RuneCountInString(title) > goalMaxTitleRune {
		return methods.GoalDTO{}, fmt.Errorf("title too long")
	}
	now := time.Now().UTC()
	criteriaItems := []methods.GoalCriterionDTO{{ID: "criterion-1", Description: criteria, Status: "unknown"}}
	criteriaJSON, _ := json.Marshal(criteriaItems)
	row := model.Goal{
		SessionID:    sessionID,
		Title:        title,
		Objective:    objective,
		Status:       "pending",
		CriteriaJSON: string(criteriaJSON),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	created, err := s.repos.Goals.Create(row)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	_ = s.appendGoalEvent(created, "", "created", "Goal contract created by user", map[string]any{
		"objective": created.Objective, "criteria": criteriaItems,
	})
	return s.goalDTO(created), nil
}

// BindToRun activates or resumes a goal for a run. Explicit only (no default bind).
func (s GoalService) BindToRun(sessionID, runID, goalID string, continueGoal bool) (methods.GoalDTO, error) {
	if err := s.RepairStaleActive(sessionID); err != nil {
		return methods.GoalDTO{}, err
	}
	var row model.Goal
	var err error
	switch {
	case strings.TrimSpace(goalID) != "":
		row, err = s.repos.Goals.Get(goalID)
	case continueGoal:
		row, err = s.repos.Goals.GetLatestPausedBySession(sessionID)
		if err != nil {
			row, err = s.repos.Goals.GetLatestPendingBySession(sessionID)
		}
	default:
		return methods.GoalDTO{}, fmt.Errorf("goal_id or continue_goal required")
	}
	if err != nil {
		return methods.GoalDTO{}, fmt.Errorf("goal not found for bind")
	}
	if row.SessionID != sessionID {
		return methods.GoalDTO{}, fmt.Errorf("goal not found for bind")
	}
	switch row.Status {
	case "succeeded", "failed", "cancelled":
		return methods.GoalDTO{}, fmt.Errorf("goal is terminal")
	case "active":
		if row.ActiveRunID != "" && row.ActiveRunID != runID {
			return methods.GoalDTO{}, fmt.Errorf("goal already bound to another run")
		}
	case "pending", "paused":
		// ok
	default:
		return methods.GoalDTO{}, fmt.Errorf("goal status %q cannot bind", row.Status)
	}
	// Ensure no other active goal.
	if active, aErr := s.repos.Goals.GetActiveBySession(sessionID); aErr == nil && active.ID != row.ID {
		return methods.GoalDTO{}, fmt.Errorf("session already has an active goal")
	}
	fromStatus := row.Status
	fromActiveRunID := ""
	if row.Status == "active" {
		fromActiveRunID = row.ActiveRunID
	}
	now := time.Now().UTC()
	row.Status = "active"
	row.ActiveRunID = runID
	row.LastRunID = runID
	row.PauseReason = ""
	if row.StartedAt == nil {
		row.StartedAt = &now
	}
	row.UpdatedAt = now
	changed, err := s.repos.Goals.TransitionStatus(row.ID, sessionID, []string{fromStatus}, fromActiveRunID, map[string]any{
		"status":        row.Status,
		"active_run_id": row.ActiveRunID,
		"last_run_id":   row.LastRunID,
		"pause_reason":  row.PauseReason,
		"started_at":    row.StartedAt,
	})
	if err != nil {
		return methods.GoalDTO{}, err
	}
	if !changed {
		return methods.GoalDTO{}, fmt.Errorf("goal changed concurrently")
	}
	updated, err := s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	_ = s.appendGoalEvent(updated, runID, "bound", "Goal execution lease acquired", map[string]any{"from_status": fromStatus})
	// Persist run.goal_id best-effort.
	if run, rErr := s.repos.Runs.Get(runID); rErr == nil {
		run.GoalID = updated.ID
		_ = s.repos.DB.Model(&model.RunRecord{}).Where("id = ?", runID).Update("goal_id", updated.ID).Error
	}
	return s.goalDTO(updated), nil
}

func (s GoalService) PauseByRun(runID, reason string) error {
	row, err := s.repos.Goals.FindByActiveRunID(runID)
	if err != nil {
		// Try last_run_id
		row, err = s.repos.Goals.FindByLastRunID(runID)
		if err != nil {
			return nil
		}
	}
	if row.Status != "active" {
		return nil
	}
	now := time.Now().UTC()
	row.Status = "paused"
	row.PauseReason = reason
	row.LastRunID = runID
	row.ActiveRunID = ""
	row.UpdatedAt = now
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, runID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": runID, "active_run_id": "",
	})
	if err == nil && changed {
		row.Status = "paused"
		_ = s.appendGoalEvent(row, runID, "paused", "Goal execution paused", map[string]any{"reason": reason})
	}
	return err
}

func (s GoalService) PauseBySession(sessionID, reason string) error {
	row, err := s.repos.Goals.GetActiveBySession(sessionID)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	activeRunID := row.ActiveRunID
	row.Status = "paused"
	row.PauseReason = reason
	if row.ActiveRunID != "" {
		row.LastRunID = row.ActiveRunID
	}
	row.ActiveRunID = ""
	row.UpdatedAt = now
	changed, err := s.repos.Goals.TransitionStatus(row.ID, sessionID, []string{"active"}, activeRunID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": row.LastRunID, "active_run_id": "",
	})
	if err == nil && changed {
		_ = s.appendGoalEvent(row, activeRunID, "paused", "Goal execution paused", map[string]any{"reason": reason})
	}
	return err
}

func (s GoalService) OnRootRunTerminal(runID, sessionID, finishStatus string) error {
	row, err := s.resolveGoalForRun(runID, sessionID)
	if err != nil {
		return nil
	}
	if wall, wallErr := s.repos.Goals.CalculateUsedWallTime(row.ID); wallErr == nil {
		_ = s.repos.DB.Model(&model.Goal{}).Where("id = ?", row.ID).Update("used_wall_time_sec", wall).Error
		row.UsedWallTimeSec = wall
	}
	if row.Status != "active" {
		return nil
	}
	now := time.Now().UTC()
	// Budget exhausted already terminalized via segment_end.
	if finishStatus == "budget_exhausted" {
		row.Status = "failed"
		row.FailReason = "budget_exhausted"
		row.FinishedAt = &now
	} else if row.UsedToolTurns >= row.MaxTotalToolTurns && row.MaxTotalToolTurns > 0 {
		row.Status = "failed"
		row.FailReason = "budget_exhausted"
		row.FinishedAt = &now
	} else if row.MaxWallTimeSec > 0 && row.UsedWallTimeSec >= row.MaxWallTimeSec {
		row.Status = "failed"
		row.FailReason = "budget_exhausted"
		row.FinishedAt = &now
	} else {
		row.Status = "paused"
		switch strings.ToLower(finishStatus) {
		case "cancelled":
			if row.PauseReason == "" {
				row.PauseReason = "user_cancel"
			}
		case "failed", "error":
			row.PauseReason = "run_failed"
		case "denied":
			row.PauseReason = "permission_denied"
		default:
			row.PauseReason = "awaiting_continue"
		}
	}
	row.LastRunID = runID
	row.ActiveRunID = ""
	row.UpdatedAt = now
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, runID, map[string]any{
		"status":             row.Status,
		"pause_reason":       row.PauseReason,
		"fail_reason":        row.FailReason,
		"last_run_id":        row.LastRunID,
		"active_run_id":      "",
		"finished_at":        row.FinishedAt,
		"used_wall_time_sec": row.UsedWallTimeSec,
	})
	if err == nil && changed {
		kind := "paused"
		if row.Status == "failed" {
			kind = "failed"
		}
		_ = s.appendGoalEvent(row, runID, kind, "Root run ended", map[string]any{
			"finish_status": finishStatus, "pause_reason": row.PauseReason, "fail_reason": row.FailReason,
		})
	}
	return err
}

func (s GoalService) RepairStaleActive(sessionID string) error {
	row, err := s.repos.Goals.GetActiveBySession(sessionID)
	if err != nil {
		return nil
	}
	if row.ActiveRunID == "" {
		return nil
	}
	run, err := s.repos.Runs.Get(row.ActiveRunID)
	if err != nil {
		return s.forcePauseStale(row, "stale_run")
	}
	switch run.Status {
	case "running", "waiting_permission":
		return nil
	default:
		return s.forcePauseStale(row, "stale_run")
	}
}

func (s GoalService) forcePauseStale(row model.Goal, reason string) error {
	now := time.Now().UTC()
	activeRunID := row.ActiveRunID
	row.Status = "paused"
	row.PauseReason = reason
	if row.ActiveRunID != "" {
		row.LastRunID = row.ActiveRunID
	}
	row.ActiveRunID = ""
	row.UpdatedAt = now
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, activeRunID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": row.LastRunID, "active_run_id": "",
	})
	if err == nil && changed {
		_ = s.appendGoalEvent(row, activeRunID, "paused", "Stale execution lease repaired", map[string]any{"reason": reason})
	}
	return err
}

func (s GoalService) resolveGoalForRun(runID, sessionID string) (model.Goal, error) {
	if run, err := s.repos.Runs.Get(runID); err == nil && run.GoalID != "" {
		if g, gErr := s.repos.Goals.Get(run.GoalID); gErr == nil {
			return g, nil
		}
	}
	if g, err := s.repos.Goals.FindByActiveRunID(runID); err == nil {
		return g, nil
	}
	return s.repos.Goals.FindByLastRunID(runID)
}

func (s GoalService) executeList(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	items, err := s.ListBySession(params.SessionID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("goal.list", nil, items, map[string]any{"action": "list", "count": len(items)}),
		Goals:  items,
	}, nil
}

func (s GoalService) executeSegmentEnd(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	row, err := s.goalForTool(params.SessionID, params.RunID, goalID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	// Active goals may only be accounted by the bound run (A1/A3).
	if row.Status == "active" && row.ActiveRunID != "" && row.ActiveRunID != params.RunID {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is not bound to this run")
	}
	delta := intArgFromMap(params.Arguments, "delta_tool_turns", 0)
	segmentIndex := intArgFromMap(params.Arguments, "segment_index", -1)
	if delta < 0 {
		delta = 0
	}
	budgetFlag := boolArgFromMap(params.Arguments, "budget_exhausted", false)
	recorded := false
	// Record ledger when there is work to account, or a non-budget boundary
	// needs a zero-delta segment marker. budget_exhausted-only reports may skip
	// ledger insert (segment_index may be absent).
	if delta > 0 || !budgetFlag {
		if segmentIndex < 0 {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("segment_index is required")
		}
		// Terminal goals: allow idempotent replay of an already-recorded segment;
		// reject brand-new segment keys (enforced inside RecordSegment).
		recorded, err = s.repos.Goals.RecordSegment(row.ID, params.RunID, segmentIndex, delta)
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
	}
	row, err = s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	budgetExhausted := row.MaxTotalToolTurns > 0 && row.UsedToolTurns >= row.MaxTotalToolTurns
	budgetExhausted = budgetExhausted || budgetFlag
	if budgetExhausted && row.Status == "active" {
		// Prefer CAS against the calling run when it owns the goal; if ActiveRunID
		// was already cleared, TransitionStatus with empty filter still works.
		activeRunFilter := params.RunID
		if row.ActiveRunID == "" {
			activeRunFilter = ""
		} else if row.ActiveRunID != params.RunID {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is not bound to this run")
		}
		now := time.Now().UTC()
		_, err = s.repos.Goals.TransitionStatus(row.ID, params.SessionID, []string{"active"}, activeRunFilter, map[string]any{
			"status": "failed", "fail_reason": "budget_exhausted", "active_run_id": "", "last_run_id": params.RunID, "finished_at": &now,
		})
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		row, err = s.repos.Goals.Get(row.ID)
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
	}
	dto := s.goalDTO(row)
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("segment_end", &dto, nil, map[string]any{"action": "segment_end", "delta": delta, "recorded": recorded}),
		Goal:   &dto,
	}, nil
}

func (s GoalService) boundActiveGoal(sessionID, runID, goalID string) (model.Goal, error) {
	var row model.Goal
	var err error
	if goalID != "" {
		row, err = s.repos.Goals.Get(goalID)
	} else {
		row, err = s.repos.Goals.FindByActiveRunID(runID)
	}
	if err != nil || row.SessionID != sessionID {
		return model.Goal{}, fmt.Errorf("goal not found")
	}
	if row.Status != "active" || row.ActiveRunID != runID {
		return model.Goal{}, fmt.Errorf("goal is not active for this run")
	}
	return row, nil
}

func (s GoalService) goalForTool(sessionID, runID, goalID string) (model.Goal, error) {
	if goalID != "" {
		row, err := s.repos.Goals.Get(goalID)
		if err != nil || row.SessionID != sessionID {
			return model.Goal{}, fmt.Errorf("goal not found")
		}
		return row, nil
	}
	if g, err := s.repos.Goals.FindByActiveRunID(runID); err == nil && g.SessionID == sessionID {
		return g, nil
	}
	if g, err := s.repos.Goals.GetActiveBySession(sessionID); err == nil {
		return g, nil
	}
	return model.Goal{}, fmt.Errorf("goal not found")
}

func (s GoalService) goalDTO(row model.Goal) methods.GoalDTO {
	dto := methods.GoalDTO{
		ID:                row.ID,
		SessionID:         row.SessionID,
		Title:             row.Title,
		Objective:         row.Objective,
		Status:            row.Status,
		PauseReason:       row.PauseReason,
		FailReason:        row.FailReason,
		ReportMarkdown:    row.ReportMarkdown,
		UsedToolTurns:     row.UsedToolTurns,
		MaxTotalToolTurns: row.MaxTotalToolTurns,
		UsedSegments:      row.UsedSegments,
		MaxSegmentsPerRun: row.MaxSegmentsPerRun,
		MaxToolTurnsSeg:   row.MaxToolTurnsPerSegment,
		UsedWallTimeSec:   row.UsedWallTimeSec,
		MaxWallTimeSec:    row.MaxWallTimeSec,
		ActiveRunID:       row.ActiveRunID,
		LastRunID:         row.LastRunID,
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:         row.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Criteria:          criteriaFromRow(row),
		Constraints:       constraintsFromRow(row),
		Strategy:          row.Strategy,
		CurrentActionID:   row.CurrentActionID,
		CurrentAction:     row.CurrentAction,
		LastObservation:   row.LastObservation,
		LastAssessment:    assessmentFromRow(row),
		LastDecision:      row.LastDecision,
		OutcomeSummary:    row.OutcomeSummary,
		Iteration:         row.Iteration,
		MaxIterations:     row.MaxIterations,
		StagnationCount:   row.StagnationCount,
		MaxStagnation:     row.MaxStagnation,
		Version:           row.Version,
	}
	actions, err := s.repos.Goals.ListActions(row.ID)
	if err == nil {
		dto.Actions = goalActionDTOs(actions)
	}
	return dto
}

func runtimeGoalOutput(tool string, goal *methods.GoalDTO, goals []methods.GoalDTO, meta map[string]any) string {
	payload := map[string]any{
		"tool": tool,
		"meta": meta,
	}
	if goal != nil {
		payload["goal"] = goal
	}
	if len(goals) > 0 {
		payload["goals"] = goals
	}
	return marshalRuntimeToolJSON(payload, tool+" ok")
}

func boolArgFromMap(args map[string]any, key string, def bool) bool {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes"
	default:
		return def
	}
}

// stringArgFromMap / intArgFromMap may already exist in todo.go — reuse if same package.
