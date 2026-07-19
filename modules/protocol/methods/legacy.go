package methods

import "strings"

// Legacy / internal tool names (docs/47 Wave E prep).
//
// Compatibility window:
//   - Prefer CanonicalToolName at every Runtime tool entry before policy/dispatch.
//   - Prefer StateToolExecute over per-domain *.tool.execute RPCs for new code.
//   - segment_end is an internal Goal budget RPC name, not a model-visible tool.
// Removal of wire aliases requires an explicit product/version decision (Wave E cutover).

const (
	// LegacyToolAliasTodoWrite is the pre-todo.write alias still accepted on wire.
	// Deprecated: models and new code should emit "todo.write" only.
	LegacyToolAliasTodoWrite = "todo_write"

	// ToolTodoWrite is the canonical todo write tool name.
	ToolTodoWrite = "todo.write"
	// ToolTodoList is the canonical todo list tool name.
	ToolTodoList = "todo.list"

	// InternalGoalSegmentEnd is the Runtime→Gateway budget accounting tool name.
	// It is not registered in the provider tool schema. Prefer this constant over
	// string literals so a future private method rename is a single-file change.
	InternalGoalSegmentEnd = "segment_end"
)

// CanonicalToolName maps legacy tool aliases to stable schema names.
// Unknown names are returned trimmed unchanged.
func CanonicalToolName(name string) string {
	switch strings.TrimSpace(name) {
	case LegacyToolAliasTodoWrite:
		return ToolTodoWrite
	default:
		return strings.TrimSpace(name)
	}
}

// IsLegacyToolAlias reports whether name is a deprecated alias that still works.
func IsLegacyToolAlias(name string) bool {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	return CanonicalToolName(trimmed) != trimmed
}

// IsInternalGoalBudgetTool reports whether name is the Goal segment budget path
// (not exposed to the model tool surface).
func IsInternalGoalBudgetTool(name string) bool {
	return strings.TrimSpace(name) == InternalGoalSegmentEnd
}

// IsTodoWriteTool reports whether name is the todo write tool (canonical or legacy).
func IsTodoWriteTool(name string) bool {
	return CanonicalToolName(name) == ToolTodoWrite
}
