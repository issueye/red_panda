package methods

import "strings"

// Compatibility cutover (docs/47 Wave E-cutover, BREAKING).
//
// Removed wire acceptance:
//   - tool alias "todo_write" (use "todo.write")
//   - domain RPCs memory/todo/schedule.tool.execute (use StateToolExecute)
//   - internal budget tool name "segment_end" (Goal feature removed)
//
// Domain Params/Result types remain — they are the typed service envelopes
// projected from StateToolExecuteParams, not separate wire methods.

const (
	// ToolTodoWrite is the only accepted todo write tool name.
	ToolTodoWrite = "todo.write"
	// ToolTodoList is the only accepted todo list tool name.
	ToolTodoList = "todo.list"

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
