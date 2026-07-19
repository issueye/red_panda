package methods

import "strings"

// Compatibility cutover (docs/47 Wave E-cutover, BREAKING).
//
// Removed wire acceptance:
//   - tool alias "todo_write" (use "todo.write")
//   - domain RPCs memory/todo/goal/context.tool.execute (use StateToolExecute)
//   - internal budget tool name "segment_end" (use InternalGoalSegmentEnd)
//
// Domain Params/Result types remain — they are the typed service envelopes
// projected from StateToolExecuteParams, not separate wire methods.

const (
	// ToolTodoWrite is the only accepted todo write tool name.
	ToolTodoWrite = "todo.write"
	// ToolTodoList is the only accepted todo list tool name.
	ToolTodoList = "todo.list"

	// InternalGoalSegmentEnd is the Runtime→Gateway budget accounting tool name.
	// Not registered in the provider tool schema. Wire value is goal-namespaced
	// so domain inference stays on the goal.* prefix without a special case.
	InternalGoalSegmentEnd = "goal.segment_budget"

	// RemovedLegacyTodoWriteAlias documents the retired alias (no longer accepted).
	RemovedLegacyTodoWriteAlias = "todo_write"
	// RemovedLegacySegmentEnd documents the retired internal budget name.
	RemovedLegacySegmentEnd = "segment_end"
)

// CanonicalToolName returns the stable tool name. Post-cutover there are no
// alias rewrites — names are only trimmed. Kept as a single call site so future
// renames stay centralized.
func CanonicalToolName(name string) string {
	return strings.TrimSpace(name)
}

// IsTodoWriteTool reports whether name is the canonical todo write tool.
func IsTodoWriteTool(name string) bool {
	return CanonicalToolName(name) == ToolTodoWrite
}

// IsInternalGoalBudgetTool reports whether name is the Goal segment budget path.
func IsInternalGoalBudgetTool(name string) bool {
	return CanonicalToolName(name) == InternalGoalSegmentEnd
}

// IsRemovedToolAlias reports whether name was a previously accepted alias that
// is now rejected (useful for clear error messages and tests).
func IsRemovedToolAlias(name string) bool {
	switch CanonicalToolName(name) {
	case RemovedLegacyTodoWriteAlias, RemovedLegacySegmentEnd:
		return true
	default:
		return false
	}
}
