# Code Directness Optimization Plan

Updated: 2026-07-15
Status: Wave 1 complete; Wave 2/3 planned
Baseline: `3544c7a`

## 1. Objective

Make the implementation easier to follow and change without altering public behavior. The first optimization wave targets long coordinator functions, repeated rollback/projection code, and tool metadata spread across multiple control paths.

Success means:

- core workflows read as short, named steps;
- error cleanup has one path instead of repeated branches;
- frontend hooks have narrow responsibilities and smaller dependency surfaces;
- tool definition, timeout, and dispatch metadata cannot silently drift;
- existing HTTP, WebSocket, JSON-RPC, UI, and persistence behavior remains compatible;
- focused tests and the repository CI gate pass.

## 2. Guardrails

- Do not add new product features during structural refactoring.
- Do not change public method names, payload fields, event ordering, or persisted schemas.
- Prefer extraction of cohesive helpers over new framework-style abstractions.
- Keep orchestration visible: helpers should name real domain steps, not hide the whole workflow behind a generic pipeline.
- Do not combine unrelated formatting or UI redesign with these changes.
- Preserve user changes and keep each parallel workstream inside its assigned files.

## 3. Baseline Assessment

| Area | Evidence | Main cost |
| --- | --- | --- |
| Gateway run start | `service/run.go` `Start` combines admission, persistence, context assembly, rollback, and dispatch | repeated cleanup and lock handling |
| Desktop session actions | `useSessionActions.js` accepts a large dependency set and combines CRUD, compaction, commands, Goal recovery, and run control | broad hook ownership and long command branching |
| Runtime tools | `tools/runner.go` keeps schema, timeout policy, and dispatch in separate sections | metadata drift and repeated tool-name groups |
| Goal loop | `runtime/goal_loop.go` combines deadlines, budget calculation, hydration, reporting, and continuation | high branch density |
| Desktop shell | `App.jsx` remains a large coordinator with broad prop plumbing | difficult ownership and review surface |
| Compatibility paths | `todo_write`, legacy protocol markers, and old planner/subagent documentation remain | duplicate terminology and branches |

## 4. Work Waves

### Wave 1 - Parallel coordinator simplification

#### W1-A Gateway run start

Primary files:

- `modules/gateway/internal/gateway/service/run.go` (+ `run_start.go` / `run_goal.go` / `run_events.go` after docs/41 W5-4)
- focused service tests

Implementation:

1. Extract atomic run admission and guarantee unlock with `defer`.
2. Centralize post-admission failure cleanup.
3. Extract run parameter construction/context preparation where it shortens `Start`.
4. Keep Goal pause semantics and Runtime dispatch ordering unchanged.

Status: **done** — `Start` is admission → prepare → dispatch; helpers live in `run_start.go`; Goal bind in `run_goal.go`; event projection in `run_events.go` (docs/41 W5-4).

Acceptance:

- `Start` reads as admission -> preparation -> dispatch;
- no repeated manual unlock branches;
- no repeated Goal pause + Run finish blocks;
- Gateway service and full Gateway tests pass.

#### W1-B Desktop session actions

Primary files:

- `modules/desktop/frontend/src/hooks/useSessionActions.js`
- new focused hooks or action modules under the same directory
- focused frontend tests

Implementation:

1. Separate session/workspace CRUD, compaction, and run/command actions.
2. Extract repeated local message/projection builders.
3. Preserve the existing `useSessionActions` facade if that keeps `App.jsx` stable during this wave.
4. Keep command behavior, auto-compact behavior, Goal resume, and diagnostics unchanged.

Acceptance:

- no single action hook owns all three domains;
- `sendTask` is decomposed into named command/run handlers;
- current public facade and UI behavior remain compatible;
- frontend unit tests pass.

#### W1-C Runtime tool registry

Primary files:

- `modules/agent/internal/tools/runner.go`
- new registry files in `modules/agent/internal/tools`
- focused tool tests

Implementation:

1. Introduce a single internal source for stable tool definitions and timeout classes.
2. Reduce repeated tool-name lists across availability and timeout policy.
3. Keep handlers explicit and preserve MCP dynamic fallback.
4. Do not change tool schemas, risks, names, or permission behavior.

Acceptance:

- stable tool metadata is defined once or generated from one registry;
- aliases and dynamic MCP behavior remain covered;
- Agent tool and full Agent tests pass.

### Wave 2 - Runtime and Desktop follow-up

Start only after Wave 1 is integrated and green.

- Extract pure Goal budget/deadline/continuation decisions from `goal_loop.go`.
- Move App permission, activity loading, and right-panel behavior into focused hooks.
- Replace broad Settings prop plumbing with domain resource objects.
- Split Provider transport, stream decoding, prompt policy, and Echo provider files.
- Split `app.css` by feature using zero-style-diff moves first.

### Wave 3 - Compatibility cleanup

Start only with an explicit compatibility window decision.

- remove the `todo_write` alias at one protocol boundary instead of checking it in every layer;
- remove unused legacy protocol markers;
- align README and current design documents with Worker/Assignment terminology;
- remove confirmed dead commented UI and obsolete planner/subagent paths.

## 5. Parallel Ownership

| Worker | Scope | Must not edit |
| --- | --- | --- |
| Gateway | W1-A | frontend and Agent tool files |
| Desktop | W1-B | Gateway and Agent Go files |
| Runtime tools | W1-C | Gateway and frontend files |
| Root integrator | plan, review, conflict resolution, full verification | feature behavior outside this plan |

Workers must report changed files, behavioral invariants, tests run, and remaining risks. The root integrator reviews every diff before accepting it.

## 6. Verification Gates

Focused gates:

```text
go test ./modules/gateway/internal/gateway/service/...
go test ./modules/agent/internal/tools/...
cd modules/desktop/frontend && npm test
```

Integration gate:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/ci-gate.ps1
```

Additional audit:

- inspect `git diff --check`;
- compare public tool definitions before and after W1-C;
- verify no new compatibility aliases or generic coordinator abstractions were introduced;
- confirm working tree contains only planned changes.

## 7. Completion Criteria

Wave 1 is complete only when all three workstreams are integrated, reviewed against their acceptance criteria, and the integration gate passes. The broader optimization objective remains active until the selected later waves are either implemented or explicitly deferred with rationale in this document.

## 8. Progress Log

### 2026-07-15 - Wave 1 implementation

| Workstream | Result | Verification |
| --- | --- | --- |
| W1-A Gateway | `RunService.Start` now reads as `admitRun -> prepareRun -> dispatchRun`; admission unlock and failure cleanup are centralized | focused and full Gateway tests |
| W1-B Desktop | `useSessionActions` is a stable thin facade over CRUD, compaction, and run/command hooks; repeated projection helpers have focused tests | 142 frontend unit tests and production build |
| W1-C Runtime tools | 39 stable public tool definitions and timeout classes are derived from one registry; explicit dispatch and dynamic MCP fallback remain | focused and full Agent tests plus definition/order comparison |

Wave 2 and Wave 3 are intentionally not mixed into this structural batch. They remain follow-up work because Goal-loop extraction, App/Provider/CSS decomposition, and compatibility removal have separate behavior and release risks.
