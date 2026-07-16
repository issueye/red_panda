package service

// Goal V2 feedback controller tool handlers (create/plan/observe/assess/finish)
// and projection helpers. Lifecycle/bind/pause remain in goal.go (docs/41 W5-3).
// File renamed from goal_v2.go — there is no dual V1 controller path.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

const goalMaxActions = 50
const goalMaxCriteria = 20
const goalMaxConstraints = 20

func (s GoalService) ListJournal(sessionID, goalID string) ([]methods.GoalEventDTO, error) {
	goal, err := s.repos.Goals.Get(strings.TrimSpace(goalID))
	if err != nil || goal.SessionID != strings.TrimSpace(sessionID) {
		return nil, fmt.Errorf("goal not found")
	}
	rows, err := s.repos.Goals.ListEvents(goal.ID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]methods.GoalEventDTO, 0, len(rows))
	for _, row := range rows {
		payload := map[string]any{}
		_ = json.Unmarshal([]byte(row.Payload), &payload)
		out = append(out, methods.GoalEventDTO{
			ID: row.ID, GoalID: row.GoalID, RunID: row.RunID, Seq: row.Seq,
			Kind: row.Kind, Summary: row.Summary, Payload: payload,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

func (s GoalService) executeCreateV2(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	objective := strings.TrimSpace(stringArgFromMap(params.Arguments, "objective"))
	if objective == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("objective is required")
	}
	if utf8.RuneCountInString(objective) > goalMaxObjectiveRune {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("objective too long")
	}
	if _, err := s.repos.Sessions.Get(params.SessionID); err != nil {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("session not found")
	}
	if err := s.RepairStaleActive(params.SessionID); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if _, err := s.repos.Goals.GetActiveBySession(params.SessionID); err == nil {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("session already has an active goal")
	}
	count, err := s.repos.Goals.CountBySession(params.SessionID)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if count >= goalMaxSessionRows {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("session goal limit %d reached", goalMaxSessionRows)
	}

	criteria, err := criteriaFromArgs(params.Arguments)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	constraints, err := stringListArg(params.Arguments, "constraints")
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	criteriaJSON, _ := json.Marshal(criteria)
	constraintsJSON, _ := json.Marshal(constraints)
	title := strings.TrimSpace(stringArgFromMap(params.Arguments, "title"))
	if title == "" {
		title = truncateRunes(objective, 40)
	}
	now := time.Now().UTC()
	row := model.Goal{
		SessionID: params.SessionID, Title: title, Objective: objective,
		Status:       "active",
		CriteriaJSON: string(criteriaJSON), ConstraintsJSON: string(constraintsJSON),
		Strategy:    strings.TrimSpace(stringArgFromMap(params.Arguments, "strategy")),
		ActiveRunID: params.RunID, LastRunID: params.RunID, SourceRunID: params.RunID,
		SourceToolCallID: params.ToolCallID, StartedAt: &now, CreatedAt: now, UpdatedAt: now,
		MaxIterations: intArgFromMap(params.Arguments, "max_iterations", 0),
		MaxStagnation: intArgFromMap(params.Arguments, "max_stagnation", 0),
	}
	created, err := s.repos.Goals.Create(row)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if err := s.repos.DB.Model(&model.RunRecord{}).Where("id = ?", params.RunID).Update("goal_id", created.ID).Error; err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	_ = s.appendGoalEvent(created, params.RunID, "created", "Goal contract created", map[string]any{
		"objective": objective, "criteria": criteria, "constraints": constraints,
	})
	dto := s.goalDTO(created)
	return goalResult("goal.create", "create", dto), nil
}

func (s GoalService) executePlanV2(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id")))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	strategy := strings.TrimSpace(stringArgFromMap(params.Arguments, "strategy"))
	actions, err := actionsFromArgs(row, params.Arguments)
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if strategy == "" && actions == nil {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("strategy or actions is required")
	}
	if actions != nil {
		if err := s.repos.Goals.ReplaceActions(row, actions); err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		row.CurrentActionID = ""
		row.CurrentAction = ""
		for _, action := range actions {
			if action.Status == "active" {
				row.CurrentActionID = action.ID
				row.CurrentAction = action.Title
				break
			}
		}
	}
	if strategy != "" {
		row.Strategy = truncateRunes(strategy, 8000)
	}
	row.LastDecision = truncateRunes(strings.TrimSpace(stringArgFromMap(params.Arguments, "decision")), 4000)
	row.Version++
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, params.RunID, map[string]any{
		"strategy": row.Strategy, "current_action_id": row.CurrentActionID,
		"current_action": row.CurrentAction, "last_decision": row.LastDecision, "version": row.Version,
	})
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if !changed {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	_ = s.appendGoalEvent(row, params.RunID, "planned", "Goal action queue updated", map[string]any{
		"strategy": row.Strategy, "actions": actions, "decision": row.LastDecision,
	})
	updated, _ := s.repos.Goals.Get(row.ID)
	dto := s.goalDTO(updated)
	return goalResult("goal.plan", "plan", dto), nil
}

func (s GoalService) executeObserveV2(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id")))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	observation := strings.TrimSpace(stringArgFromMap(params.Arguments, "observation"))
	evidence := strings.TrimSpace(stringArgFromMap(params.Arguments, "evidence"))
	if observation == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("observation is required")
	}
	action, hasAction, err := s.findGoalAction(row.ID, strings.TrimSpace(stringArgFromMap(params.Arguments, "action_id")))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if hasAction {
		rawStatus := strings.TrimSpace(stringArgFromMap(params.Arguments, "action_status"))
		status := normalizeActionStatus(rawStatus)
		if rawStatus != "" && status == "" {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("invalid action_status")
		}
		if status == "" {
			status = action.Status
		}
		if (status == "done" || status == "blocked") && evidence == "" {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("evidence is required when an action is done or blocked")
		}
		action.Status = status
		action.Result = truncateRunes(observation, 8000)
		action.Evidence = truncateRunes(evidence, 12000)
		action.Attempt++
		if status == "done" || status == "blocked" || status == "dropped" {
			now := time.Now().UTC()
			action.FinishedAt = &now
		}
		if err := s.repos.Goals.UpdateAction(action); err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		if status == "done" || status == "blocked" || status == "dropped" {
			row.CurrentActionID = ""
			row.CurrentAction = ""
		}
	}
	row.LastObservation = truncateRunes(observation, 8000)
	row.Version++
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, params.RunID, map[string]any{
		"last_observation": row.LastObservation, "current_action_id": row.CurrentActionID,
		"current_action": row.CurrentAction, "version": row.Version,
	})
	if err != nil || !changed {
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	_ = s.appendGoalEvent(row, params.RunID, "observed", observation, map[string]any{
		"action_id": action.ID, "evidence": evidence, "action_status": action.Status,
	})
	updated, _ := s.repos.Goals.Get(row.ID)
	dto := s.goalDTO(updated)
	return goalResult("goal.observe", "observe", dto), nil
}

func (s GoalService) executeAssessV2(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id")))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	verdict := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "verdict")))
	switch verdict {
	case "progress", "satisfied", "blocked", "no_progress":
	default:
		return methods.GoalToolExecuteResult{}, fmt.Errorf("verdict must be progress, satisfied, blocked, or no_progress")
	}
	summary := strings.TrimSpace(stringArgFromMap(params.Arguments, "summary"))
	evidence := strings.TrimSpace(stringArgFromMap(params.Arguments, "evidence"))
	decision := strings.TrimSpace(stringArgFromMap(params.Arguments, "decision"))
	if summary == "" || evidence == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("summary and evidence are required")
	}
	criteria, err := assessedCriteria(params.Arguments, criteriaFromRow(row))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	if verdict == "satisfied" && !allCriteriaMet(criteria) {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("satisfied requires every success criterion to be met with evidence")
	}
	if verdict == "progress" && decision == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("progress assessment requires the next decision")
	}
	assessment := methods.GoalAssessmentDTO{
		Verdict: verdict, Summary: truncateRunes(summary, 4000), Gap: truncateRunes(strings.TrimSpace(stringArgFromMap(params.Arguments, "gap")), 4000),
		Decision: truncateRunes(decision, 4000), Criteria: criteria,
		ActionID:     strings.TrimSpace(stringArgFromMap(params.Arguments, "action_id")),
		ActionStatus: normalizeActionStatus(strings.TrimSpace(stringArgFromMap(params.Arguments, "action_status"))),
		Evidence:     truncateRunes(evidence, 12000),
	}
	assessmentJSON, _ := json.Marshal(assessment)
	criteriaJSON, _ := json.Marshal(criteria)
	row.Iteration++
	if verdict == "no_progress" {
		row.StagnationCount++
	} else if verdict == "progress" || verdict == "satisfied" {
		row.StagnationCount = 0
	}
	row.LastAssessmentJSON = string(assessmentJSON)
	row.CriteriaJSON = string(criteriaJSON)
	row.LastDecision = assessment.Decision
	row.Version++
	status := "active"
	pauseReason := ""
	activeRunID := row.ActiveRunID
	if verdict == "blocked" {
		status, pauseReason, activeRunID = "paused", "blocked", ""
	} else if row.MaxStagnation > 0 && row.StagnationCount >= row.MaxStagnation {
		status, pauseReason, activeRunID = "paused", "stagnated", ""
	} else if row.MaxIterations > 0 && row.Iteration >= row.MaxIterations && verdict != "satisfied" {
		status, pauseReason, activeRunID = "paused", "iteration_limit", ""
	}
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, params.RunID, map[string]any{
		"criteria_json": row.CriteriaJSON, "last_assessment_json": row.LastAssessmentJSON,
		"last_decision": row.LastDecision, "iteration": row.Iteration,
		"stagnation_count": row.StagnationCount, "version": row.Version,
		"status": status, "pause_reason": pauseReason, "active_run_id": activeRunID,
		"last_run_id": params.RunID,
	})
	if err != nil || !changed {
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	_ = s.appendGoalEvent(row, params.RunID, "assessed", summary, assessment)
	updated, _ := s.repos.Goals.Get(row.ID)
	dto := s.goalDTO(updated)
	return goalResult("goal.assess", "assess", dto), nil
}

func (s GoalService) executeFinishV2(params methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	row, err := s.boundActiveGoal(params.SessionID, params.RunID, strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id")))
	if err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	status := strings.ToLower(strings.TrimSpace(stringArgFromMap(params.Arguments, "status")))
	if status != "succeeded" && status != "failed" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("status must be succeeded or failed")
	}
	summary := strings.TrimSpace(stringArgFromMap(params.Arguments, "summary"))
	report := strings.TrimSpace(stringArgFromMap(params.Arguments, "report_markdown"))
	if summary == "" || report == "" {
		return methods.GoalToolExecuteResult{}, fmt.Errorf("summary and report_markdown are required")
	}
	assessment := assessmentFromRow(row)
	if status == "succeeded" {
		if assessment == nil || assessment.Verdict != "satisfied" || !allCriteriaMet(assessment.Criteria) {
			return methods.GoalToolExecuteResult{}, fmt.Errorf("successful finish requires a persisted satisfied assessment")
		}
	}
	now := time.Now().UTC()
	changed, err := s.repos.Goals.TransitionStatus(row.ID, row.SessionID, []string{"active"}, params.RunID, map[string]any{
		"status": status, "active_run_id": "", "last_run_id": params.RunID,
		"outcome_summary": truncateRunes(summary, 4000), "report_markdown": truncateRunes(report, 20000),
		"fail_reason": func() string {
			if status == "failed" {
				return truncateRunes(summary, 4000)
			}
			return ""
		}(),
		"finished_at": &now, "version": row.Version + 1,
	})
	if err != nil || !changed {
		if err != nil {
			return methods.GoalToolExecuteResult{}, err
		}
		return methods.GoalToolExecuteResult{}, fmt.Errorf("goal is no longer active for this run")
	}
	row.Status = status
	_ = s.appendGoalEvent(row, params.RunID, "finished", summary, map[string]any{"status": status, "report": report})
	updated, _ := s.repos.Goals.Get(row.ID)
	dto := s.goalDTO(updated)
	return goalResult("goal.finish", "finish", dto), nil
}

func goalResult(tool, action string, dto methods.GoalDTO) methods.GoalToolExecuteResult {
	return methods.GoalToolExecuteResult{Status: "completed", Output: runtimeGoalOutput(tool, &dto, nil, map[string]any{"action": action}), Goal: &dto}
}

func (s GoalService) appendGoalEvent(goal model.Goal, runID, kind, summary string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.repos.Goals.AppendEvent(model.GoalEvent{
		GoalID: goal.ID, SessionID: goal.SessionID, RunID: runID,
		Kind: kind, Summary: truncateRunes(summary, 4000), Payload: string(raw),
	})
	return err
}

func criteriaFromArgs(args map[string]any) ([]methods.GoalCriterionDTO, error) {
	if raw, ok := args["criteria"]; ok && raw != nil {
		items, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("criteria must be an array")
		}
		if len(items) == 0 || len(items) > goalMaxCriteria {
			return nil, fmt.Errorf("criteria must contain 1 to %d items", goalMaxCriteria)
		}
		out := make([]methods.GoalCriterionDTO, 0, len(items))
		seen := map[string]bool{}
		for i, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("criterion %d must be an object", i+1)
			}
			desc := strings.TrimSpace(stringArgFromMap(m, "description"))
			if desc == "" {
				return nil, fmt.Errorf("criterion %d description is required", i+1)
			}
			if utf8.RuneCountInString(desc) > 2000 {
				return nil, fmt.Errorf("criterion %d description is too long", i+1)
			}
			id := strings.TrimSpace(stringArgFromMap(m, "id"))
			if id == "" {
				id = fmt.Sprintf("criterion-%d", i+1)
			}
			if seen[id] {
				return nil, fmt.Errorf("criterion id %q is duplicated", id)
			}
			seen[id] = true
			out = append(out, methods.GoalCriterionDTO{ID: id, Description: desc, Status: "unknown"})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("at least one criterion is required")
		}
		return out, nil
	}
	return nil, fmt.Errorf("criteria is required")
}

func stringListArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	if len(items) > goalMaxConstraints {
		return nil, fmt.Errorf("%s limit %d reached", key, goalMaxConstraints)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			if utf8.RuneCountInString(text) > 1000 {
				return nil, fmt.Errorf("%s item is too long", key)
			}
			out = append(out, text)
		}
	}
	return out, nil
}

func actionsFromArgs(goal model.Goal, args map[string]any) ([]model.GoalAction, error) {
	raw, ok := args["actions"]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("actions must be an array")
	}
	if len(items) > goalMaxActions {
		return nil, fmt.Errorf("goal action limit %d reached", goalMaxActions)
	}
	now := time.Now().UTC()
	out := make([]model.GoalAction, 0, len(items))
	active := 0
	keys := map[string]bool{}
	ids := map[string]bool{}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("action %d must be an object", i+1)
		}
		title := strings.TrimSpace(stringArgFromMap(m, "title"))
		if title == "" {
			return nil, fmt.Errorf("action %d title is required", i+1)
		}
		key := strings.TrimSpace(stringArgFromMap(m, "key"))
		if key == "" {
			key = fmt.Sprintf("action-%d", i+1)
		}
		if keys[key] {
			return nil, fmt.Errorf("action key %q is duplicated", key)
		}
		keys[key] = true
		rawStatus := strings.TrimSpace(stringArgFromMap(m, "status"))
		status := normalizeActionStatus(rawStatus)
		if rawStatus != "" && status == "" {
			return nil, fmt.Errorf("action %q has invalid status", key)
		}
		if status == "" {
			status = "queued"
		}
		if status == "active" {
			active++
		}
		id := fmt.Sprintf("goal_action_%s_%s", goal.ID, safeGoalKey(key))
		if ids[id] {
			return nil, fmt.Errorf("action keys must remain unique after normalization")
		}
		ids[id] = true
		out = append(out, model.GoalAction{
			ID: id, GoalID: goal.ID, SessionID: goal.SessionID,
			ActionKey: key, Title: truncateRunes(title, 300), Description: truncateRunes(strings.TrimSpace(stringArgFromMap(m, "description")), 4000),
			Acceptance: truncateRunes(strings.TrimSpace(stringArgFromMap(m, "acceptance")), 4000), Status: status,
			SortOrder: i, CreatedAt: now, UpdatedAt: now,
		})
	}
	if active > 1 {
		return nil, fmt.Errorf("at most one goal action may be active")
	}
	if active == 0 && len(out) > 0 {
		out[0].Status = "active"
	}
	return out, nil
}

func safeGoalKey(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "action"
	}
	return b.String()
}

func normalizeActionStatus(value string) string {
	switch strings.ToLower(value) {
	case "queued", "active", "done", "blocked", "dropped":
		return strings.ToLower(value)
	}
	return ""
}

func (s GoalService) findGoalAction(goalID, actionID string) (model.GoalAction, bool, error) {
	actions, err := s.repos.Goals.ListActions(goalID)
	if err != nil {
		return model.GoalAction{}, false, err
	}
	if actionID == "" {
		for _, action := range actions {
			if action.Status == "active" {
				return action, true, nil
			}
		}
		return model.GoalAction{}, false, nil
	}
	for _, action := range actions {
		if action.ID == actionID || action.ActionKey == actionID {
			return action, true, nil
		}
	}
	return model.GoalAction{}, false, fmt.Errorf("goal action not found")
}

func assessedCriteria(args map[string]any, contract []methods.GoalCriterionDTO) ([]methods.GoalCriterionDTO, error) {
	raw, ok := args["criteria"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("criteria assessment is required")
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("criteria must be an array")
	}
	byID := make(map[string]methods.GoalCriterionDTO, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("criterion assessment %d must be an object", i+1)
		}
		id := strings.TrimSpace(stringArgFromMap(m, "id"))
		status := strings.ToLower(strings.TrimSpace(stringArgFromMap(m, "status")))
		evidence := strings.TrimSpace(stringArgFromMap(m, "evidence"))
		switch status {
		case "unknown", "met", "not_met", "blocked":
		default:
			return nil, fmt.Errorf("criterion %q has invalid status", id)
		}
		if id == "" || evidence == "" {
			return nil, fmt.Errorf("criterion assessment id and evidence are required")
		}
		if _, exists := byID[id]; exists {
			return nil, fmt.Errorf("criterion %q was assessed more than once", id)
		}
		byID[id] = methods.GoalCriterionDTO{ID: id, Status: status, Evidence: truncateRunes(evidence, 8000)}
	}
	out := make([]methods.GoalCriterionDTO, 0, len(contract))
	for _, criterion := range contract {
		assessed, ok := byID[criterion.ID]
		if !ok {
			return nil, fmt.Errorf("criterion %q was not assessed", criterion.ID)
		}
		criterion.Status, criterion.Evidence = assessed.Status, assessed.Evidence
		out = append(out, criterion)
	}
	return out, nil
}

func criteriaFromRow(row model.Goal) []methods.GoalCriterionDTO {
	var out []methods.GoalCriterionDTO
	if json.Unmarshal([]byte(row.CriteriaJSON), &out) == nil && len(out) > 0 {
		return out
	}
	return []methods.GoalCriterionDTO{{ID: "criterion-1", Description: "The requested outcome is implemented and verified.", Status: "unknown"}}
}

func constraintsFromRow(row model.Goal) []string {
	var out []string
	_ = json.Unmarshal([]byte(row.ConstraintsJSON), &out)
	return out
}
func assessmentFromRow(row model.Goal) *methods.GoalAssessmentDTO {
	if row.LastAssessmentJSON == "" {
		return nil
	}
	var out methods.GoalAssessmentDTO
	if json.Unmarshal([]byte(row.LastAssessmentJSON), &out) != nil {
		return nil
	}
	return &out
}
func allCriteriaMet(criteria []methods.GoalCriterionDTO) bool {
	if len(criteria) == 0 {
		return false
	}
	for _, c := range criteria {
		if c.Status != "met" || strings.TrimSpace(c.Evidence) == "" {
			return false
		}
	}
	return true
}
func criteriaText(criteria []methods.GoalCriterionDTO) string {
	parts := make([]string, 0, len(criteria))
	for _, c := range criteria {
		parts = append(parts, c.Description)
	}
	return strings.Join(parts, "\n")
}

func goalActionDTOs(rows []model.GoalAction) []methods.GoalActionDTO {
	out := make([]methods.GoalActionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, methods.GoalActionDTO{
			ID: row.ID, Key: row.ActionKey, Title: row.Title,
			Description: row.Description, Acceptance: row.Acceptance,
			Status: row.Status, Result: row.Result, Evidence: row.Evidence,
			Attempt: row.Attempt, SortOrder: row.SortOrder,
		})
	}
	return out
}

func formatGoalContextV2(goal methods.GoalDTO) string {
	var b strings.Builder
	b.WriteString("Goal controller state:\n")
	b.WriteString(fmt.Sprintf("- id: %s\n- status: %s\n- objective: %s\n", goal.ID, goal.Status, goal.Objective))
	if len(goal.Criteria) > 0 {
		b.WriteString("- success criteria:\n")
		for _, criterion := range goal.Criteria {
			b.WriteString(fmt.Sprintf("  - [%s] %s", firstGoalValue(criterion.Status, "unknown"), criterion.Description))
			if criterion.Evidence != "" {
				b.WriteString(": " + truncateRunes(criterion.Evidence, 300))
			}
			b.WriteString("\n")
		}
	}
	if len(goal.Constraints) > 0 {
		b.WriteString("- constraints: " + strings.Join(goal.Constraints, "; ") + "\n")
	}
	if goal.Strategy != "" {
		b.WriteString("- strategy: " + truncateRunes(goal.Strategy, 500) + "\n")
	}
	if goal.CurrentAction != "" {
		b.WriteString(fmt.Sprintf("- current action: %s (%s)\n", goal.CurrentAction, goal.CurrentActionID))
	}
	if goal.LastObservation != "" {
		b.WriteString("- last observation: " + truncateRunes(goal.LastObservation, 400) + "\n")
	}
	if goal.LastAssessment != nil {
		b.WriteString(fmt.Sprintf("- last assessment: %s — %s\n", goal.LastAssessment.Verdict, truncateRunes(goal.LastAssessment.Summary, 400)))
		if goal.LastAssessment.Gap != "" {
			b.WriteString("- remaining gap: " + truncateRunes(goal.LastAssessment.Gap, 400) + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("- control budget: iteration %d/%d, stagnation %d/%d, tool turns %d/%d\n",
		goal.Iteration, goal.MaxIterations, goal.StagnationCount, goal.MaxStagnation, goal.UsedToolTurns, goal.MaxTotalToolTurns))
	b.WriteString("Use goal.plan to choose/revise actions, goal.observe after acting, goal.assess against every criterion, and goal.finish only after a satisfied assessment.\n")
	return b.String()
}

func firstGoalValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
