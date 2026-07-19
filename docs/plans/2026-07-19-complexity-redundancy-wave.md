# Complexity & Redundancy Wave — Implementation Plan

> **For implementers:** Execute task-by-task. Prefer behavior-preserving refactors with focused tests.

**Goal:** Lower Goal-loop coordinator complexity and state-tool boilerplate without changing public protocol or default tool surfaces.

**Architecture:** Extract pure decision helpers from `runWithGoalLoop`; share Gateway runtime-tool JSON marshaling; normalize legacy tool aliases at one boundary.

**Tech Stack:** Go modules (`redpanda/agent`, `redpanda/gateway`, `redpanda/protocol`); existing table-driven tests.

**Parent doc:** [docs/47-complexity-redundancy-development-plan.md](../47-complexity-redundancy-development-plan.md)

---

### Task 1: Pure segment tool-limit helper

**Files:**
- Create: `modules/agent/internal/runtime/goal_loop_decisions.go`
- Test: `modules/agent/internal/runtime/goal_loop_decisions_test.go`

**Step 1: Add `effectiveSegmentToolLimit`**

```go
// effectiveSegmentToolLimit returns the per-segment MaxToolTurns cap for the
// next provider segment. Total budget remaining can only tighten MaxToolTurnsSeg.
func effectiveSegmentToolLimit(goal methods.GoalDTO) int {
	limit := goal.MaxToolTurnsSeg
	if goal.MaxTotalToolTurns > 0 {
		remaining := goal.MaxTotalToolTurns - goal.UsedToolTurns
		if remaining < 1 {
			remaining = 1
		}
		if limit <= 0 || remaining < limit {
			limit = remaining
		}
	}
	return limit
}
```

**Step 2: Table tests** — seg limit only; total remaining tighter; remaining < 1 clamps to 1; zero total ignores remaining.

**Step 3:** `go test ./modules/agent/internal/runtime/ -count=1 -run EffectiveSegment`

---

### Task 2: Pure after-segment continue decision

**Files:**
- Modify: `modules/agent/internal/runtime/goal_loop_decisions.go`
- Test: `modules/agent/internal/runtime/goal_loop_decisions_test.go`

**Step 1: Types + function**

```go
type goalLoopDecision struct {
	Continue bool
	MaxSeg   int
	// StopReason is informational when Continue is false (empty = use last.Reason).
	StopReason loopEndReason
}

func decideGoalLoopContinue(
	state *runGoalState,
	lastReason loopEndReason,
	seg int,
	maxSeg int,
) goalLoopDecision
```

Encode existing rules from `runWithGoalLoop` (hard fails, terminal, unbound single-seg, total budget, soft boundary only, maxSeg ceiling, expand maxSeg when bound).

**Step 2: Table tests** covering each branch.

**Step 3:** Wire into `runWithGoalLoop` — replace inline if-tree with `decideGoalLoopContinue`.

**Step 4:**

```text
go test ./modules/agent/internal/runtime/ -count=1 -run "Goal|Segment|Loop|Decide"
```

---

### Task 3: Wire hydrate + limit call sites

**Files:**
- Modify: `modules/agent/internal/runtime/goal_loop.go`

**Step 1:** Use `effectiveSegmentToolLimit` where MaxToolTurns is set.

**Step 2:** Use `initialGoalMaxSegments(state)` if extracted.

**Step 3:** Full runtime package test (skip external web).

---

### Task 4: Shared Gateway JSON marshal

**Files:**
- Create: `modules/gateway/internal/gateway/service/runtime_tool_output.go`
- Modify: `memory.go` `runtimeMemoryOutput`, `todo.go` `runtimeTodoOutput`, `goal.go` `runtimeGoalOutput`

**Step 1:**

```go
func marshalRuntimeToolJSON(payload map[string]any, fallback string) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fallback
	}
	return string(raw)
}
```

**Step 2:** Delegate without changing payload keys.

**Step 3:**

```text
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Todo|Memory|Goal|StateTool"
```

---

### Task 5: Normalize `todo_write` at tool runner entry

**Files:**
- Modify: `modules/agent/internal/tools/helpers.go` or new `aliases.go`
- Modify: `modules/agent/internal/tools/runner.go` (`RunWithContext` or `dispatchTool`)

**Step 1:** `func CanonicalToolName(name string) string` — map `todo_write` → `todo.write`.

**Step 2:** Call once before policy/dispatch so case lists can prefer canonical names over time.

**Step 3:** Keep Gateway `todo_write` case for external callers this wave.

**Step 4:** `go test ./modules/agent/internal/tools/ -count=1`

---

### Task 6: Update plan status + optional commit

- Mark Wave A/B rows done in docs/47.
- Commit only if user requests.

---

## Verification matrix

| After | Command |
| --- | --- |
| Task 1–3 | `go test ./modules/agent/internal/runtime/ -count=1 -skip "Web\|Duck\|Tavily"` |
| Task 4 | `go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Todo\|Memory\|Goal\|State"` |
| Task 5 | `go test ./modules/agent/internal/tools/ -count=1` |
| Merge | `go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...` |
