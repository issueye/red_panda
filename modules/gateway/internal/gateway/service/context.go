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
	contextMaxNoteBodyRune  = 8000
	contextMaxNoteTitleRune = 200
	contextDefaultListLimit = 50
	contextMaxListLimit     = 200
)

var contextAllowedKinds = map[string]struct{}{
	"finding":  {},
	"decision": {},
	"risk":     {},
	"fact":     {},
	"handoff":  {},
	"note":     {},
}

// ContextService exposes the goal scratchpad to the runtime via context.* tools.
type ContextService struct {
	repos repository.Set
}

func NewContextService(repos repository.Set) ContextService {
	return ContextService{repos: repos}
}

func (s ContextService) ExecuteRuntimeTool(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	if err := validateRuntimeToolMeta(s.repos, params.RunID, params.SessionID, params.ToolCallID, true); err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	name := strings.TrimSpace(params.ToolName)
	switch name {
	case "context.read":
		return s.executeRead(params)
	case "context.search":
		return s.executeSearch(params)
	case "context.write":
		return s.executeWrite(params)
	case "context.replace":
		return s.executeReplace(params)
	case "context.delete":
		return s.executeDelete(params)
	default:
		return methods.ContextToolExecuteResult{}, fmt.Errorf("unsupported context tool %q", name)
	}
}

func (s ContextService) executeRead(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	goalID, _, err := s.requireAccessibleGoal(params)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	limit := intArgFromMap(params.Arguments, "limit", contextDefaultListLimit)
	opts := repository.NoteListOpts{Limit: limit}
	if kind := strings.TrimSpace(stringArgFromMap(params.Arguments, "kind")); kind != "" {
		opts.Kind = kind
	}
	if sinceSeq := intArgFromMap(params.Arguments, "since_seq", 0); sinceSeq > 0 {
		opts.SinceSeq = sinceSeq
	}
	if onlyPinned := boolArgFromMap(params.Arguments, "pinned_only", false); onlyPinned {
		t := true
		opts.Pinned = &t
	}
	rows, err := s.repos.Contexts.List(goalID, opts)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	return contextResult("context.read", rows), nil
}

// ListNotes returns the goal scratchpad for Desktop projection (docs/36 C4).
// Read-only; enforces goal membership of the session.
func (s ContextService) ListNotes(sessionID, goalID string, limit int) ([]methods.GoalNoteDTO, error) {
	sessionID = strings.TrimSpace(sessionID)
	goalID = strings.TrimSpace(goalID)
	if sessionID == "" || goalID == "" {
		return nil, fmt.Errorf("session_id and goal_id are required")
	}
	goal, err := s.repos.Goals.Get(goalID)
	if err != nil {
		return nil, fmt.Errorf("goal not found")
	}
	if goal.SessionID != sessionID {
		return nil, fmt.Errorf("goal does not belong to this session")
	}
	if limit <= 0 || limit > contextMaxListLimit {
		limit = contextDefaultListLimit
	}
	rows, err := s.repos.Contexts.List(goalID, repository.NoteListOpts{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]methods.GoalNoteDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, noteToDTO(r))
	}
	return out, nil
}

func (s ContextService) executeSearch(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	goalID, _, err := s.requireAccessibleGoal(params)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	query := strings.TrimSpace(stringArgFromMap(params.Arguments, "query"))
	if query == "" {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("query is required")
	}
	limit := intArgFromMap(params.Arguments, "limit", contextDefaultListLimit)
	rows, err := s.repos.Contexts.Search(goalID, query, limit)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	return contextResult("context.search", rows), nil
}

func (s ContextService) executeWrite(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	goalID, goal, err := s.requireWritableGoal(params)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	title := strings.TrimSpace(stringArgFromMap(params.Arguments, "title"))
	body := strings.TrimSpace(stringArgFromMap(params.Arguments, "body"))
	if title == "" {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(title) > contextMaxNoteTitleRune {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("title exceeds %d characters", contextMaxNoteTitleRune)
	}
	if utf8.RuneCountInString(body) > contextMaxNoteBodyRune {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("body exceeds %d characters", contextMaxNoteBodyRune)
	}
	kind := normalizeNoteKind(stringArgFromMap(params.Arguments, "kind"))
	row := model.GoalNote{
		ID:        params.ToolCallID,
		GoalID:    goalID,
		SessionID: params.SessionID,
		Kind:      kind,
		Title:     title,
		Body:      body,
		Phase:     goal.CurrentActionID,
		Source:    sourceForRun(params.RunID, goal.ActiveRunID),
		RunID:     params.RunID,
		Pinned:    boolArgFromMap(params.Arguments, "pinned", false),
	}
	saved, _, err := s.repos.Contexts.Append(row)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	return contextResult("context.write", []model.GoalNote{saved}), nil
}

func (s ContextService) executeReplace(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	goalID, goal, err := s.requireWritableGoal(params)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	title := strings.TrimSpace(stringArgFromMap(params.Arguments, "title"))
	body := strings.TrimSpace(stringArgFromMap(params.Arguments, "body"))
	if title == "" {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(title) > contextMaxNoteTitleRune {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("title exceeds %d characters", contextMaxNoteTitleRune)
	}
	if utf8.RuneCountInString(body) > contextMaxNoteBodyRune {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("body exceeds %d characters", contextMaxNoteBodyRune)
	}
	kind := normalizeNoteKind(stringArgFromMap(params.Arguments, "kind"))
	row := model.GoalNote{
		GoalID:    goalID,
		SessionID: params.SessionID,
		Kind:      kind,
		Title:     title,
		Body:      body,
		Phase:     goal.CurrentActionID,
		Source:    sourceForRun(params.RunID, goal.ActiveRunID),
		RunID:     params.RunID,
		Pinned:    boolArgFromMap(params.Arguments, "pinned", false),
	}
	saved, _, err := s.repos.Contexts.Replace(row)
	if err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	return contextResult("context.replace", []model.GoalNote{saved}), nil
}

func (s ContextService) executeDelete(params methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	if goalID == "" {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("goal_id is required")
	}
	noteID := strings.TrimSpace(stringArgFromMap(params.Arguments, "note_id"))
	if noteID == "" {
		return methods.ContextToolExecuteResult{}, fmt.Errorf("note_id is required")
	}
	if _, _, err := s.requireWritableGoal(params); err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	if err := s.repos.Contexts.Delete(goalID, noteID); err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	out, _ := json.Marshal(map[string]any{"action": "context.delete", "deleted": true, "note_id": noteID})
	return methods.ContextToolExecuteResult{Status: "completed", Output: string(out)}, nil
}

// requireAccessibleGoal loads the goal and ensures it belongs to the caller's session.
// Used for read/search so notes cannot leak across sessions by guessing goal_id.
func (s ContextService) requireAccessibleGoal(params methods.ContextToolExecuteParams) (string, model.Goal, error) {
	goalID := strings.TrimSpace(stringArgFromMap(params.Arguments, "goal_id"))
	if goalID == "" {
		return "", model.Goal{}, fmt.Errorf("goal_id is required")
	}
	goal, err := s.repos.Goals.Get(goalID)
	if err != nil {
		return "", model.Goal{}, fmt.Errorf("goal not found")
	}
	if goal.SessionID != params.SessionID {
		return "", model.Goal{}, fmt.Errorf("goal does not belong to this session")
	}
	return goalID, goal, nil
}

// requireWritableGoal loads the goal, rejects terminal goals, and authorizes the
// caller: the root active run OR a specialist child (run_id differs but still
// belongs to the goal's session). Root goal-state tools stay locked behind
// subagentRunDenylist; here we only gate scratchpad writes by session + non-terminal.
func (s ContextService) requireWritableGoal(params methods.ContextToolExecuteParams) (string, model.Goal, error) {
	goalID, goal, err := s.requireAccessibleGoal(params)
	if err != nil {
		return "", model.Goal{}, err
	}
	if isTerminalGoalStatus(goal.Status) {
		return "", model.Goal{}, fmt.Errorf("goal is terminal")
	}
	return goalID, goal, nil
}

func isTerminalGoalStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	}
	return false
}

func normalizeNoteKind(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := contextAllowedKinds[k]; !ok {
		return "note"
	}
	return k
}

// sourceForRun labels who wrote a note. The active root run is "root"; any other
// run id (specialist children) is "specialist:<run_id>".
func sourceForRun(runID string, activeRunID string) string {
	if runID == activeRunID {
		return "root"
	}
	return "specialist:" + runID
}

func contextResult(action string, rows []model.GoalNote) methods.ContextToolExecuteResult {
	dto := make([]methods.GoalNoteDTO, 0, len(rows))
	for _, r := range rows {
		dto = append(dto, noteToDTO(r))
	}
	out, _ := json.Marshal(map[string]any{
		"action": action,
		"count":  len(dto),
	})
	return methods.ContextToolExecuteResult{
		Status: "completed",
		Output: string(out),
		Notes:  dto,
	}
}

func noteToDTO(r model.GoalNote) methods.GoalNoteDTO {
	pinned := 0
	if r.Pinned {
		pinned = 1
	}
	return methods.GoalNoteDTO{
		ID:        r.ID,
		GoalID:    r.GoalID,
		Kind:      r.Kind,
		Title:     r.Title,
		Body:      r.Body,
		Phase:     r.Phase,
		Source:    r.Source,
		RunID:     r.RunID,
		Seq:       r.Seq,
		Pinned:    pinned,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
