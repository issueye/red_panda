package methods

// methods_state_tool.go — Gateway-mediated state tool DTOs and helpers.
//
// Contains the Memory/Todo/Goal/Context domain execute envelopes, the unified
// StateToolExecute envelope, and the resolve/require/as/new helpers used by
// Gateway to dispatch a single state.tool.execute RPC into the four state
// domains (docs/47 E-cutover, docs/plans/2026-07-19-convergence-wave.md Wave C Task C3).
//
// Method-name constants and the GoalNoteDTO shape remain in methods.go.

import (
	"fmt"
	"strings"
)

type MemoryToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

type MemoryToolExecuteResult struct {
	Status   string           `json:"status"`
	Output   string           `json:"output,omitempty"`
	RecordID string           `json:"record_id,omitempty"`
	Items    []MemoryToolItem `json:"items,omitempty"`
}

type MemoryToolItem struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// TodoContext is the formatted session task list for one reply turn.
type TodoContext struct {
	Items   []TodoItemDTO `json:"items,omitempty"`
	Context string        `json:"context,omitempty"`
}

// TodoItemDTO is the shared todo item shape for tools, HTTP, and events.
type TodoItemDTO struct {
	ID         string `json:"id"`
	ClientKey  string `json:"client_key,omitempty"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	SortOrder  int    `json:"sort_order"`
	Priority   string `json:"priority,omitempty"`
	ActiveForm string `json:"active_form,omitempty"`
}

// TodoToolExecuteParams is Runtime -> Gateway for todo.write / todo.list.
type TodoToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// TodoToolExecuteResult is Gateway -> Runtime for todo tools.
type TodoToolExecuteResult struct {
	Status    string        `json:"status"`
	Output    string        `json:"output,omitempty"`
	Items     []TodoItemDTO `json:"items,omitempty"`
	OpenCount int           `json:"open_count,omitempty"`
}

// GoalContext is model-facing goal state for one reply turn.
type GoalContext struct {
	GoalID            string             `json:"goal_id"`
	Title             string             `json:"title,omitempty"`
	Objective         string             `json:"objective"`
	Status            string             `json:"status"`
	UsedToolTurns     int                `json:"used_tool_turns"`
	MaxTotalToolTurns int                `json:"max_total_tool_turns"`
	UsedSegments      int                `json:"used_segments"`
	MaxSegmentsPerRun int                `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int                `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int                `json:"used_wall_time_sec"`
	MaxWallTimeSec    int                `json:"max_wall_time_sec"`
	Context           string             `json:"context,omitempty"`
	Criteria          []GoalCriterionDTO `json:"criteria,omitempty"`
	Constraints       []string           `json:"constraints,omitempty"`
	Strategy          string             `json:"strategy,omitempty"`
	CurrentActionID   string             `json:"current_action_id,omitempty"`
	CurrentAction     string             `json:"current_action,omitempty"`
	Actions           []GoalActionDTO    `json:"actions,omitempty"`
	LastObservation   string             `json:"last_observation,omitempty"`
	LastAssessment    *GoalAssessmentDTO `json:"last_assessment,omitempty"`
	LastDecision      string             `json:"last_decision,omitempty"`
	Iteration         int                `json:"iteration"`
	MaxIterations     int                `json:"max_iterations"`
	StagnationCount   int                `json:"stagnation_count"`
	MaxStagnation     int                `json:"max_stagnation"`
}

// GoalCriterionDTO is one independently assessable condition in a Goal
// contract. Status is unknown|met|not_met|blocked.
type GoalCriterionDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

// GoalActionDTO is a Goal-owned action in the controller's revisable queue.
type GoalActionDTO struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Acceptance  string `json:"acceptance,omitempty"`
	Status      string `json:"status"`
	Result      string `json:"result,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Attempt     int    `json:"attempt"`
	SortOrder   int    `json:"sort_order"`
}

// GoalAssessmentDTO is the persisted output of one feedback-control cycle.
type GoalAssessmentDTO struct {
	Verdict      string             `json:"verdict"` // progress|satisfied|blocked|no_progress
	Summary      string             `json:"summary"`
	Gap          string             `json:"gap,omitempty"`
	Decision     string             `json:"decision,omitempty"`
	Criteria     []GoalCriterionDTO `json:"criteria,omitempty"`
	ActionID     string             `json:"action_id,omitempty"`
	ActionStatus string             `json:"action_status,omitempty"`
	Evidence     string             `json:"evidence,omitempty"`
}

// GoalEventDTO is one append-only controller or lifecycle journal entry.
type GoalEventDTO struct {
	ID        string         `json:"id"`
	GoalID    string         `json:"goal_id"`
	RunID     string         `json:"run_id,omitempty"`
	Seq       int            `json:"seq"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt string         `json:"created_at"`
}

// GoalDTO is the shared goal shape for tools, HTTP, and events.
type GoalDTO struct {
	ID                string             `json:"id"`
	SessionID         string             `json:"session_id"`
	Title             string             `json:"title,omitempty"`
	Objective         string             `json:"objective"`
	Status            string             `json:"status"`
	PauseReason       string             `json:"pause_reason,omitempty"`
	FailReason        string             `json:"fail_reason,omitempty"`
	ReportMarkdown    string             `json:"report_markdown,omitempty"`
	UsedToolTurns     int                `json:"used_tool_turns"`
	MaxTotalToolTurns int                `json:"max_total_tool_turns"`
	UsedSegments      int                `json:"used_segments"`
	MaxSegmentsPerRun int                `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int                `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int                `json:"used_wall_time_sec"`
	MaxWallTimeSec    int                `json:"max_wall_time_sec"`
	ActiveRunID       string             `json:"active_run_id,omitempty"`
	LastRunID         string             `json:"last_run_id,omitempty"`
	CreatedAt         string             `json:"created_at,omitempty"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
	Criteria          []GoalCriterionDTO `json:"criteria,omitempty"`
	Constraints       []string           `json:"constraints,omitempty"`
	Strategy          string             `json:"strategy,omitempty"`
	CurrentActionID   string             `json:"current_action_id,omitempty"`
	CurrentAction     string             `json:"current_action,omitempty"`
	Actions           []GoalActionDTO    `json:"actions,omitempty"`
	LastObservation   string             `json:"last_observation,omitempty"`
	LastAssessment    *GoalAssessmentDTO `json:"last_assessment,omitempty"`
	LastDecision      string             `json:"last_decision,omitempty"`
	OutcomeSummary    string             `json:"outcome_summary,omitempty"`
	Iteration         int                `json:"iteration"`
	MaxIterations     int                `json:"max_iterations"`
	StagnationCount   int                `json:"stagnation_count"`
	MaxStagnation     int                `json:"max_stagnation"`
	Version           int                `json:"version"`
}

// GoalToolExecuteParams is Runtime -> Gateway for goal.* tools.
type GoalToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// GoalToolExecuteResult is Gateway -> Runtime for goal tools.
type GoalToolExecuteResult struct {
	Status string    `json:"status"`
	Output string    `json:"output,omitempty"`
	Goal   *GoalDTO  `json:"goal,omitempty"`
	Goals  []GoalDTO `json:"goals,omitempty"`
	// CancelRunID is set when a goal tool terminalized an active Goal that had a
	// bound run. Gateway cancels that run after the tool response is returned
	// (async) so the tool RPC cannot deadlock against AgentCancel.
	CancelRunID string `json:"cancel_run_id,omitempty"`
}

// ContextToolExecuteParams is Runtime -> Gateway for context.* tools (goal scratchpad).
type ContextToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// ContextToolExecuteResult is Gateway -> Runtime for context tools.
type ContextToolExecuteResult struct {
	Status string        `json:"status"`
	Output string        `json:"output,omitempty"`
	Notes  []GoalNoteDTO `json:"notes,omitempty"`
}

// StateToolExecuteParams is the unified Runtime → Gateway envelope for all four
// Gateway-mediated state domains (docs/41 W2-3). Domain may be omitted when
// ToolName carries a recognizable prefix (memory.*, todo.*, goal.*, context.*).
// Domain services still return their existing typed results; JSON fields stay
// compatible with Memory/Todo/Goal/ContextToolExecuteResult unmarshaling.
type StateToolExecuteParams struct {
	Domain        string         `json:"domain,omitempty"`
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// ResolveStateToolDomain returns the canonical domain for a state tool call.
// Prefer explicit domain; otherwise infer from tool_name / legacy aliases.
func ResolveStateToolDomain(domain, toolName string) (string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	switch domain {
	case StateToolDomainMemory, StateToolDomainTodo, StateToolDomainGoal, StateToolDomainContext, StateToolDomainSchedule:
		return domain, nil
	case "":
		// infer below
	default:
		return "", fmt.Errorf("unsupported state tool domain %q", domain)
	}
	name := CanonicalToolName(toolName)
	if IsRemovedToolAlias(name) {
		return "", fmt.Errorf("tool alias %q was removed; use the canonical tool name (docs/47 E-cutover)", name)
	}
	switch {
	case name == ToolTodoWrite, name == ToolTodoList, strings.HasPrefix(name, "todo."):
		return StateToolDomainTodo, nil
	case strings.HasPrefix(name, "goal."):
		// Includes InternalGoalSegmentEnd ("goal.segment_budget").
		return StateToolDomainGoal, nil
	case strings.HasPrefix(name, "memory."):
		return StateToolDomainMemory, nil
	case strings.HasPrefix(name, "context."):
		return StateToolDomainContext, nil
	case strings.HasPrefix(name, "schedule."):
		return StateToolDomainSchedule, nil
	default:
		return "", fmt.Errorf("cannot resolve state tool domain for tool %q", name)
	}
}

// StateToolRequiresSession reports whether the domain requires a live session_id
// in the Runtime→Gateway envelope (memory keeps workspace-scoped ownership).
func StateToolRequiresSession(domain string) bool {
	switch strings.TrimSpace(strings.ToLower(domain)) {
	case StateToolDomainMemory:
		return false
	default:
		return true
	}
}

// AsMemoryParams projects the unified envelope onto the memory domain params.
func (p StateToolExecuteParams) AsMemoryParams() MemoryToolExecuteParams {
	return MemoryToolExecuteParams{
		RunID: p.RunID, SessionID: p.SessionID, WorkspaceRoot: p.WorkspaceRoot,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments,
	}
}

// AsTodoParams projects the unified envelope onto the todo domain params.
func (p StateToolExecuteParams) AsTodoParams() TodoToolExecuteParams {
	return TodoToolExecuteParams{
		RunID: p.RunID, SessionID: p.SessionID, WorkspaceRoot: p.WorkspaceRoot,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments,
	}
}

// AsGoalParams projects the unified envelope onto the goal domain params.
func (p StateToolExecuteParams) AsGoalParams() GoalToolExecuteParams {
	return GoalToolExecuteParams{
		RunID: p.RunID, SessionID: p.SessionID, WorkspaceRoot: p.WorkspaceRoot,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments,
	}
}

// AsContextParams projects the unified envelope onto the context domain params.
func (p StateToolExecuteParams) AsContextParams() ContextToolExecuteParams {
	return ContextToolExecuteParams{
		RunID: p.RunID, SessionID: p.SessionID, WorkspaceRoot: p.WorkspaceRoot,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments,
	}
}

// NewStateToolParams builds a unified envelope from common Runtime tool fields.
func NewStateToolParams(domain, runID, sessionID, workspaceRoot, toolCallID, toolName string, arguments map[string]any) StateToolExecuteParams {
	return StateToolExecuteParams{
		Domain:        domain,
		RunID:         runID,
		SessionID:     sessionID,
		WorkspaceRoot: workspaceRoot,
		ToolCallID:    toolCallID,
		ToolName:      toolName,
		Arguments:     arguments,
	}
}
