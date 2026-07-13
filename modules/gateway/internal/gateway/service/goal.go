package service

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
		out = append(out, goalDTO(row))
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
	return goalDTO(row), nil
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
		return goalDTO(row), nil
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
		return goalDTO(updated), nil
	}
	return goalDTO(updated), nil
}

func (s GoalService) ExecuteRuntimeTool(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	if err := validateRuntimeToolMeta(s.repos, params.RunID, params.SessionID, params.ToolCallID, true); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	name := strings.TrimSpace(params.ToolName)
	switch name {
	case "goal.write":
		return s.executeWrite(params)
	case "goal.update":
		return s.executeUpdate(params)
	case "goal.checkpoint":
		return s.executeCheckpoint(params)
	case "goal.complete":
		return s.executeComplete(params)
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
	ctx := formatGoalContextText(row)
	if utf8.RuneCountInString(ctx) > goalContextLimit {
		runes := []rune(ctx)
		ctx = string(runes[:goalContextLimit]) + "…"
	}
	currentStep := ""
	if todos, tErr := s.repos.Todos.ListOpenBySession(sessionID); tErr == nil {
		for _, t := range todos {
			if t.Status == "in_progress" {
				currentStep = t.Content
				break
			}
		}
	}
	return &methods.GoalContext{
		GoalID:            row.ID,
		Title:             row.Title,
		Objective:         row.Objective,
		SuccessCriteria:   row.SuccessCriteria,
		Status:            row.Status,
		PipelinePhase:     row.PipelinePhase,
		AnalysisSummary:   row.AnalysisSummary,
		CheckpointSummary: row.CheckpointSummary,
		ProgressNote:      row.ProgressNote,
		CurrentStep:       currentStep,
		UsedToolTurns:     row.UsedToolTurns,
		MaxTotalToolTurns: row.MaxTotalToolTurns,
		UsedSegments:      row.UsedSegments,
		MaxSegmentsPerRun: row.MaxSegmentsPerRun,
		MaxToolTurnsSeg:   row.MaxToolTurnsPerSegment,
		UsedWallTimeSec:   row.UsedWallTimeSec,
		MaxWallTimeSec:    row.MaxWallTimeSec,
		Context:           ctx,
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
	row := model.Goal{
		SessionID:       sessionID,
		Title:           title,
		Objective:       objective,
		SuccessCriteria: criteria,
		Status:          "pending",
		PipelinePhase:   "analyze",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	created, err := s.repos.Goals.Create(row)
	if err != nil {
		return methods.GoalDTO{}, err
	}
	return goalDTO(created), nil
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
	if row.PipelinePhase == "" {
		row.PipelinePhase = "analyze"
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
	// Persist run.goal_id best-effort.
	if run, rErr := s.repos.Runs.Get(runID); rErr == nil {
		run.GoalID = updated.ID
		_ = s.repos.DB.Model(&model.RunRecord{}).Where("id = ?", runID).Update("goal_id", updated.ID).Error
	}
	return goalDTO(updated), nil
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
	_, err = s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, runID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": runID, "active_run_id": "",
	})
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
	_, err = s.repos.Goals.TransitionStatus(row.ID, sessionID, []string{"active"}, activeRunID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": row.LastRunID, "active_run_id": "",
	})
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
	_, err = s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, runID, map[string]any{
		"status":             row.Status,
		"pause_reason":       row.PauseReason,
		"fail_reason":        row.FailReason,
		"last_run_id":        row.LastRunID,
		"active_run_id":      "",
		"finished_at":        row.FinishedAt,
		"used_wall_time_sec": row.UsedWallTimeSec,
	})
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
	_, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, activeRunID, map[string]any{
		"status": "paused", "pause_reason": reason, "last_run_id": row.LastRunID, "active_run_id": "",
	})
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

func (s GoalService) executeWrite(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	objective := strings.TrimSpace(stringArgFromMap(params.Arguments, "objective"))
	if objective == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("objective is required")
	}
	if utf8.RuneCountInString(objective) > goalMaxObjectiveRune {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("objective too long")
	}
	n, err := s.repos.Goals.CountBySession(params.SessionID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if n >= goalMaxSessionRows {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("session goal limit %d reached", goalMaxSessionRows)
	}
	activate := boolArgFromMap(params.Arguments, "activate", false)
	criteria := strings.TrimSpace(stringArgFromMap(params.Arguments, "success_criteria"))
	if activate && criteria == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("success_criteria is required when activate=true")
	}
	if utf8.RuneCountInString(criteria) > goalMaxCriteriaRune {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("success_criteria too long")
	}
	title := strings.TrimSpace(stringArgFromMap(params.Arguments, "title"))
	if title == "" {
		title = truncateRunes(objective, 40)
	}
	if utf8.RuneCountInString(title) > goalMaxTitleRune {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("title too long")
	}
	if activate {
		if _, err := s.repos.Goals.GetActiveBySession(params.SessionID); err == nil {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("active goal exists")
		}
	}
	now := time.Now().UTC()
	row := model.Goal{
		SessionID:        params.SessionID,
		Title:            title,
		Objective:        objective,
		SuccessCriteria:  criteria,
		Status:           "pending",
		PipelinePhase:    "plan",
		AnalysisSummary:  strings.TrimSpace(stringArgFromMap(params.Arguments, "analysis_summary")),
		SourceRunID:      params.RunID,
		SourceToolCallID: params.ToolCallID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if activate {
		row.Status = "active"
		row.ActiveRunID = params.RunID
		row.LastRunID = params.RunID
		row.StartedAt = &now
		if row.AnalysisSummary != "" {
			row.PipelinePhase = "plan"
		} else {
			row.PipelinePhase = "analyze"
		}
	}
	created, err := s.repos.Goals.Create(row)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if activate {
		_ = s.repos.DB.Model(&model.RunRecord{}).Where("id = ?", params.RunID).Update("goal_id", created.ID).Error
	}
	dto := goalDTO(created)
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("goal.write", &dto, nil, map[string]any{"action": "write", "activate": activate}),
		Goal:   &dto,
	}, nil
}

func (s GoalService) executeUpdate(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	if goalID == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal_id is required")
	}
	row, err := s.repos.Goals.Get(goalID)
	if err != nil || row.SessionID != params.SessionID {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal not found")
	}
	if action := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "action"))); action == "cancel" {
		if row.Status == "active" && row.ActiveRunID != params.RunID {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is not bound to this run")
		}
		// Capture before CancelGoal clears active_run_id (invariant: cancel stops the run).
		boundRunID := row.ActiveRunID
		dto, err := s.CancelGoal(params.SessionID, goalID)
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		res := methods.GoalToolExecuteResult{
			Status: "completed",
			Output: runtimeGoalOutput("goal.update", &dto, nil, map[string]any{"action": "cancel"}),
			Goal:   &dto,
		}
		if boundRunID != "" && dto.Status == "cancelled" {
			res.CancelRunID = boundRunID
		}
		return res, nil
	}
	if row.Status == "succeeded" || row.Status == "failed" || row.Status == "cancelled" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is terminal")
	}
	if row.Status == "active" && row.ActiveRunID != params.RunID {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is not bound to this run")
	}
	// CAS filter must use the pre-mutation binding: only already-active goals
	// pin active_run_id. pending/paused activate must not require a prior bind.
	casActiveRunID := ""
	if row.Status == "active" {
		casActiveRunID = row.ActiveRunID
	}
	fromStatuses := []string{row.Status}
	if v := strings.TrimSpace(stringArgFromMap(params.Arguments, "title")); v != "" {
		row.Title = truncateRunes(v, goalMaxTitleRune)
	}
	if v := strings.TrimSpace(stringArgFromMap(params.Arguments, "objective")); v != "" {
		row.Objective = v
	}
	if v := strings.TrimSpace(stringArgFromMap(params.Arguments, "success_criteria")); v != "" {
		row.SuccessCriteria = v
	}
	if v := strings.TrimSpace(stringArgFromMap(params.Arguments, "analysis_summary")); v != "" {
		row.AnalysisSummary = v
	}
	if v := strings.TrimSpace(stringArgFromMap(params.Arguments, "pipeline_phase")); v != "" {
		row.PipelinePhase = normalizePipelinePhase(v)
	}
	if boolArgFromMap(params.Arguments, "activate", false) {
		if row.Status == "pending" || row.Status == "paused" {
			if active, aErr := s.repos.Goals.GetActiveBySession(params.SessionID); aErr == nil && active.ID != row.ID {
				return methods.GoalToolExecuteResult{}, fmt.Errorf("active goal exists")
			}
			if row.SuccessCriteria == "" {
				return methods.GoalToolExecuteResult{}, fmt.Errorf("success_criteria required to activate")
			}
			now := time.Now().UTC()
			row.Status = "active"
			row.ActiveRunID = params.RunID
			row.LastRunID = params.RunID
			row.PauseReason = ""
			if row.StartedAt == nil {
				row.StartedAt = &now
			}
		}
	}
	changed, err := s.repos.Goals.TransitionStatus(row.ID, params.SessionID,
		fromStatuses, casActiveRunID, map[string]any{
			"title":            row.Title,
			"objective":        row.Objective,
			"success_criteria": row.SuccessCriteria,
			"analysis_summary": row.AnalysisSummary,
			"pipeline_phase":   row.PipelinePhase,
			"status":           row.Status,
			"active_run_id":    row.ActiveRunID,
			"last_run_id":      row.LastRunID,
			"pause_reason":     row.PauseReason,
			"started_at":       row.StartedAt,
		})
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if !changed {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal changed concurrently")
	}
	if row.Status == "active" {
		if err := s.repos.DB.Model(&model.RunRecord{}).Where("id = ?", params.RunID).Update("goal_id", row.ID).Error; err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
	}
	updated, err := s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	dto := goalDTO(updated)
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("goal.update", &dto, nil, map[string]any{"action": "update"}),
		Goal:   &dto,
	}, nil
}

func (s GoalService) executeCheckpoint(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, goalID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	summary := strings.TrimSpace(stringArgFromMap(params.Arguments, "summary"))
	if summary == "" {
		summary = strings.TrimSpace(stringArgFromMap(params.Arguments, "checkpoint_summary"))
	}
	if summary == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("summary is required")
	}
	row.CheckpointSummary = truncateRunes(summary, goalMaxObjectiveRune)
	if note := strings.TrimSpace(stringArgFromMap(params.Arguments, "progress_note")); note != "" {
		row.ProgressNote = truncateRunes(note, 2000)
	}
	if phase := strings.TrimSpace(stringArgFromMap(params.Arguments, "pipeline_phase")); phase != "" {
		row.PipelinePhase = normalizePipelinePhase(phase)
	}
	changed, err := s.repos.Goals.TransitionStatus(row.ID, params.SessionID,
		[]string{"active"}, params.RunID, map[string]any{
			"checkpoint_summary": row.CheckpointSummary,
			"progress_note":      row.ProgressNote,
			"pipeline_phase":     row.PipelinePhase,
		})
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if !changed {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	updated, err := s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	dto := goalDTO(updated)
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("goal.checkpoint", &dto, nil, map[string]any{"action": "checkpoint"}),
		Goal:   &dto,
	}, nil
}

func (s GoalService) executeComplete(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, goalID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	status := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "status")))
	if status == "" {
		status = "succeeded"
	}
	if status != "succeeded" && status != "failed" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("status must be succeeded or failed")
	}
	summary := strings.TrimSpace(stringArgFromMap(params.Arguments, "summary"))
	if summary == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("summary is required")
	}
	now := time.Now().UTC()
	row.Status = status
	row.ProgressNote = truncateRunes(summary, 2000)
	row.PipelinePhase = "report"
	row.ActiveRunID = ""
	row.LastRunID = params.RunID
	row.FinishedAt = &now
	if md := strings.TrimSpace(stringArgFromMap(params.Arguments, "report_markdown")); md != "" {
		row.ReportMarkdown = truncateRunes(md, 16000)
	}
	if rep, ok := params.Arguments["report"]; ok && rep != nil {
		raw, _ := json.Marshal(rep)
		row.ReportJSON = string(raw)
	}
	if status == "failed" {
		row.FailReason = summary
	}
	changed, err := s.repos.Goals.TransitionStatus(row.ID, params.SessionID,
		[]string{"active"}, params.RunID, map[string]any{
			"status":          row.Status,
			"progress_note":   row.ProgressNote,
			"pipeline_phase":  row.PipelinePhase,
			"active_run_id":   "",
			"last_run_id":     row.LastRunID,
			"finished_at":     row.FinishedAt,
			"report_markdown": row.ReportMarkdown,
			"report_json":     row.ReportJSON,
			"fail_reason":     row.FailReason,
		})
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if !changed {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	updated, err := s.repos.Goals.Get(row.ID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	dto := goalDTO(updated)
	return methods.GoalToolExecuteResult{
		Status: "completed",
		Output: runtimeGoalOutput("goal.complete", &dto, nil, map[string]any{"action": "complete", "result": status}),
		Goal:   &dto,
	}, nil
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
	if cp := strings.TrimSpace(stringArgFromMap(params.Arguments, "checkpoint_summary")); cp != "" && row.Status == "active" {
		_ = s.repos.DB.Model(&model.Goal{}).
			Where("id = ? AND status = ? AND active_run_id = ?", row.ID, "active", params.RunID).
			Update("checkpoint_summary", truncateRunes(cp, goalMaxObjectiveRune)).Error
		row, _ = s.repos.Goals.Get(row.ID)
	}
	dto := goalDTO(row)
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

func goalDTO(row model.Goal) methods.GoalDTO {
	return methods.GoalDTO{
		ID:                row.ID,
		SessionID:         row.SessionID,
		Title:             row.Title,
		Objective:         row.Objective,
		SuccessCriteria:   row.SuccessCriteria,
		Status:            row.Status,
		PipelinePhase:     row.PipelinePhase,
		PauseReason:       row.PauseReason,
		FailReason:        row.FailReason,
		AnalysisSummary:   row.AnalysisSummary,
		CheckpointSummary: row.CheckpointSummary,
		ProgressNote:      row.ProgressNote,
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
	}
}

func formatGoalContextText(row model.Goal) string {
	var b strings.Builder
	b.WriteString("Active goal (update via goal.checkpoint / goal.complete; steps via todo.write):\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", row.ID))
	b.WriteString(fmt.Sprintf("- status: %s phase: %s\n", row.Status, row.PipelinePhase))
	b.WriteString(fmt.Sprintf("- objective: %s\n", row.Objective))
	if row.SuccessCriteria != "" {
		b.WriteString(fmt.Sprintf("- success_criteria: %s\n", row.SuccessCriteria))
	}
	if row.AnalysisSummary != "" {
		b.WriteString(fmt.Sprintf("- analysis: %s\n", truncateRunes(row.AnalysisSummary, 400)))
	}
	if row.CheckpointSummary != "" {
		b.WriteString(fmt.Sprintf("- checkpoint: %s\n", truncateRunes(row.CheckpointSummary, 400)))
	}
	b.WriteString(fmt.Sprintf("- budget: %d/%d root tool turns, segments used %d (max %d/run)\n",
		row.UsedToolTurns, row.MaxTotalToolTurns, row.UsedSegments, row.MaxSegmentsPerRun))
	return b.String()
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
	raw, err := json.Marshal(payload)
	if err != nil {
		return tool + " ok"
	}
	return string(raw)
}

func normalizePipelinePhase(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "analyze", "plan", "execute", "verify", "evaluate", "report":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "execute"
	}
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
