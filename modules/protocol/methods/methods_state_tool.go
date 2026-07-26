package methods

// methods_state_tool.go — Gateway-mediated state tool DTOs and helpers.
//
// Contains the Memory/Todo domain execute envelopes, the unified
// StateToolExecute envelope, and the resolve/require/as/new helpers used by
// Gateway to dispatch a single state.tool.execute RPC into the state domains
// (docs/47 E-cutover, docs/plans/2026-07-19-convergence-wave.md Wave C Task C3).
//
// Method-name constants remain in methods.go.

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

// StateToolExecuteParams is the unified Runtime → Gateway envelope for all
// Gateway-mediated state domains (docs/41 W2-3). Domain may be omitted when
// ToolName carries a recognizable prefix (memory.*, todo.*, schedule.*).
// Domain services still return their existing typed results; JSON fields stay
// compatible with Memory/Todo/ScheduleToolExecuteResult unmarshaling.
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
	case StateToolDomainMemory, StateToolDomainTodo, StateToolDomainSchedule:
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
	case strings.HasPrefix(name, "memory."):
		return StateToolDomainMemory, nil
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
