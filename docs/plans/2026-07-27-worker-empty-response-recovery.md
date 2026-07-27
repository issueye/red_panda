# Worker Empty Response Recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent a root conversation from stopping after a tool call when the provider emits an empty final response, and stop the UI from mislabeling root recovery summaries as delegated Worker failures.

**Architecture:** Keep recovery inside the existing provider/tool loop. When a provider returns neither text nor tool calls after prior tool history, retry the normal tool-enabled loop with an explicit continuation instruction up to two times before using the existing text-only recovery and deterministic fallback. In the desktop, distinguish the root Worker profile from delegated assignments before compacting fallback messages.

**Tech Stack:** Go runtime and tests; React/JavaScript frontend utilities and Node test runner.

---

### Task 1: Reproduce the premature completion

**Files:**
- Modify: `modules/agent/internal/runtime/runtime_chain_test.go`

**Step 1: Write the failing test**

Add a provider fixture that calls `todo.write`, emits an empty final response, then only continues to a second tool when the runtime asks again with tools enabled. Assert that the second tool runs and a useful final response is emitted.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime -run TestRuntimeContinuesAfterEmptyResponseFollowingTools -count=1`

Expected: FAIL because the current runtime switches immediately to text-only recovery and completes with a deterministic tool summary.

### Task 2: Continue the provider loop safely

**Files:**
- Modify: `modules/agent/internal/runtime/loop.go`
- Test: `modules/agent/internal/runtime/runtime_chain_test.go`

**Step 1: Implement bounded continuation**

Track consecutive empty post-tool responses. For at most two attempts, compose the next normal request with an instruction to continue the unfinished task and allow tools. Each attempt consumes the existing tool-turn budget.

**Step 2: Avoid premature stream finalization**

Suppress an empty `Final` chunk when no message text has been emitted. Preserve the empty final marker when it closes an actual text response.

**Step 3: Run focused runtime tests**

Run: `go test ./internal/runtime -run "TestRuntime(ContinuesAfterEmptyResponseFollowingTools|RecoversWhenProviderReturnsEmptyAfterTools|RejectsToolCallMarkupAsFinalAnswer)" -count=1`

Expected: PASS.

### Task 3: Stop misclassifying root recovery messages

**Files:**
- Modify: `modules/desktop/frontend/src/lib/toolResultDisplay.js`
- Modify: `modules/desktop/frontend/src/lib/toolResultDisplay.test.js`

**Step 1: Write the failing frontend assertion**

Assert that a recovery summary with `profileKey: "root"` is not a delegated Worker fallback, while the same summary from a non-root assignment remains compactable.

**Step 2: Implement the root-profile guard**

Return false from `isWorkerToolFallback` when the normalized profile key is `root`.

**Step 3: Run the frontend unit test**

Run: `npm test -- --runInBand` if supported by the package, otherwise run the repository's Node test command from `modules/desktop/frontend`.

Expected: PASS.

### Task 4: Reject delegated fallback placeholders as reports

**Files:**
- Modify: `modules/agent/internal/runtime/worker_runtime.go`
- Modify: `modules/agent/internal/runtime/worker_runtime_test.go`

**Step 1: Add a recovered-fallback process fixture**

Emit a completed Worker stream containing only a message marked `recovered: true`, and assert that delegated execution rejects it as an invalid final report.

**Step 2: Connect the existing capture flag**

Check `Capture.RecoveredFallback()` before accepting `FinalText()`, returning the existing detailed assignment failure so the parent assignment retry policy can run.

**Step 3: Run the focused Worker runtime test**

Run: `go test ./internal/runtime -run TestDelegatedWorkerRejectsRecoveredFallbackAsFinalReport -count=1`

Expected: PASS.

### Task 5: Regression verification

**Files:**
- Verify: `modules/agent/internal/runtime`
- Verify: `modules/desktop/frontend/src/lib`

**Step 1: Run all Agent tests**

Run: `go test ./...` from `modules/agent`.

Expected: PASS.

**Step 2: Run frontend tests**

Run the package test script from `modules/desktop/frontend`.

Expected: PASS.

**Step 3: Review the diff**

Confirm that only the recovery loop, its tests, the frontend classifier, its tests, and this plan changed for this fix. Do not stage or alter unrelated provider-profile work already present in the worktree.
