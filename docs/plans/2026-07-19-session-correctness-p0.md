# Session Correctness P0 — Implementation Plan

**Goal:** Fix compact concurrency races and preserveLive history loss without changing product auto-compact (DelegatedOnly pause).

**Parent:** [docs/48-session-correctness-optimization-plan.md](../48-session-correctness-optimization-plan.md)

---

### Task 1: Session compact mutex

**Files:**
- Modify: `modules/gateway/internal/gateway/service/session.go`
- Test: `modules/gateway/internal/gateway/service/session_test.go`

**Steps:**
1. Add fields + `beginSessionCompact` / unlock helper on `SessionService`.
2. Wrap `Compact` and `CompactPreview` with begin/defer unlock.
3. Table/concurrent test: second call errors with `session compact already in progress`.
4. `go test ./modules/gateway/internal/gateway/service/ -count=1 -run Compact`

---

### Task 2: preserveLive merge history

**Files:**
- Modify: `modules/desktop/frontend/src/hooks/useSessionBootstrap.js`
- Modify: `modules/desktop/frontend/src/lib/sessionHistory.js` (optional afterSeq helper)
- Test: extend merge tests or bootstrap helper test

**Steps:**
1. In preserveLive branch, set `messages: mergeSessionHistoryMessages(prev.messages, normalized)`.
2. Prefer full history load (already); merge keeps live suffix.
3. Optionally refresh tools/permissions from server in preserveLive path.

---

### Task 3: Hydrate generation guard

**Files:**
- Modify: `modules/desktop/frontend/src/hooks/useSessionBootstrap.js`

**Steps:**
1. `hydrateGenBySessionRef` map.
2. Increment at start; after await, skip `patchRuntime` if gen stale.
3. Document in comment (docs/48 Wave B).

---

### Task 4: Update docs/48 progress + commit

---

## Verification

```text
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Compact|Session|Pause"
cd modules/desktop/frontend && node --test src/lib/sessionMessageMerge.test.js src/lib/sessionHistory.js 2>nul
# or full frontend unit suite
```
