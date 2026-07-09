# Structured Development Roadmap

Updated: 2026-07-09

This document controls the next development order for `red_panda`. It is meant to prevent scattered feature work by defining:

- current module status,
- priority rules,
- milestone order,
- allowed parallel work lanes,
- verification gates,
- definition of done.

For the current short-cycle execution board, use `docs/12-current-execution-plan.md`. This roadmap remains the milestone-level ordering source; the execution plan is the active work queue.

For the v0.1.0 release boundary, use `docs/16-v0.1.0-release-plan.md`. That document records the completed release gate.

For the v0.1.1 planning slice, use `docs/18-v0.1.1-development-plan.md`. v0.1.1 is a stabilization/design release centered on MCP stdio tools design, not MCP implementation.

## 1. Current System State

`red_panda` is past the skeleton phase. It is a working MVP with a real Desktop -> Gateway -> Agent Runtime loop.

| Area | Current state | Evidence | Main remaining risk |
| --- | --- | --- | --- |
| Protocol | JSON-RPC, WebSocket envelopes, run lifecycle, permissions, tools, subagents, replay, and resume are implemented. | `modules/protocol`, `scripts/protocol-compat.ps1` | External-client compatibility is still broader than current script coverage. |
| Agent Runtime | Provider abstraction, tool loop, permission gating, cancellation, in-process subagent, `runtime_process`, `process_pool`, memory context injection, and Gateway-mediated `memory.*` tools are implemented. | `modules/agent/internal/runtime`, runtime unit tests | MCP capability is not started. |
| Gateway | Gin/GORM/SQLite MVC, Runtime subprocess client, persistence, projections, provider profiles, and WebSocket routing are implemented. | `modules/gateway`, repository/service tests | Long-running process supervision and external access hardening remain shallow. |
| Desktop | Wails v3 + React UI with chat, tools, permissions, subagents, settings, workspace, activity timeline, restore, reconnect/resume, and Memory panel. | `modules/desktop/frontend`, Playwright tests | MCP and optional memory tools are not started. |
| Persistence | Sessions, messages, run events, run records, tools, permissions, provider profiles, session lineage, compactions, and memory records are persisted. | Gateway repositories and APIs | MCP configuration persistence is not started. |
| Verification | Unit tests, Playwright fixtures, Gateway-backed E2E, protocol compatibility, Wails build. | `npm test`, `npm run test:ui`, `go test`, scripts | Full smoke is slower; test matrix needs tiers to avoid wasting time. |

## 2. Development Priority Rules

When choosing the next task, use this order:

1. Stabilize existing MVP behavior before adding new major features.
2. Prefer missing user-visible workflows over internal refactors.
3. Prefer protocol-compatible additions over one-off UI-only state.
4. Add persistence and replay semantics before adding transient UI.
5. Add tests at the layer where the behavior can fail:
   - pure DTO/reducer logic: frontend unit test,
   - protocol contract: `scripts/protocol-compat.ps1`,
   - Runtime behavior: Go unit test,
   - user workflow: Playwright fixture or Gateway-backed E2E.
6. Do not expand MCP or plugin scope until memory/history backend, Runtime injection, Desktop inspection, and protocol coverage are complete or explicitly deferred.

## 3. Work Lanes

Parallel work is allowed only when write scopes are disjoint.

| Lane | Owns | Typical files | Can run in parallel with |
| --- | --- | --- | --- |
| Runtime lane | Agent behavior, provider loop, tools, subagent runtime | `modules/agent`, `modules/protocol` DTOs if needed | Desktop-only UI tests, Gateway repository work |
| Gateway lane | HTTP/WebSocket routing, persistence, projections, provider profiles | `modules/gateway`, `modules/protocol/ws` | Desktop fixture work, Runtime tests |
| Desktop lane | React UI, state restore, WebSocket client, Playwright UI | `modules/desktop/frontend` | Runtime unit work, protocol scripts if no frontend changes |
| Protocol compatibility lane | External HTTP/WebSocket scripts | `scripts/protocol-compat.ps1`, `scripts/ws-smoke.ps1` | Desktop fixture work, Runtime-only work |
| Documentation lane | Roadmap/status/design docs | `README.md`, `docs/*` | Any code lane after implementation facts are known |

Avoid parallel edits to the same files, especially:

- `modules/desktop/frontend/src/App.jsx`
- `modules/desktop/frontend/tests/gateway-backed.spec.js`
- `scripts/protocol-compat.ps1`
- `modules/agent/internal/runtime/runtime.go`
- `modules/gateway/internal/gateway/service/run.go`

## 4. Milestone Plan

### M0: Baseline Freeze

Goal: keep the current MVP reproducible.

Status: effectively complete.

Required evidence:

- `go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/... ./modules/desktop/... ./modules/cli/...`
- `npm test`
- `npm run test:ui`
- `npm run build`
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/protocol-compat.ps1`
- `wails3 build`

Rule: any future milestone must keep these passing unless the test itself is intentionally updated.

### M1: Failure-Path Closure

Goal: close the remaining MVP failure-path gaps before adding large features.

Work items, in order:

1. Gateway-backed provider profile UI failure path. Status: complete.
   - Example: inactive/deleted selected profile causes `run.start` failure and Desktop surfaces the error without losing settings state.
   - Evidence: Gateway-backed Playwright covers an inactive selected profile, visible `run.start failed` message, and preserved Settings selection.
2. Gateway-backed subagent failure/cancel visibility. Status: complete.
   - `runtime_process` startup failure is covered by Gateway-backed Playwright; SubAgentPanel and Activity timeline show failed state.
   - Running subagent cancellation is covered by Gateway-backed Playwright using `RED_PANDA_PLANNER_DRAFT_DELAY_MS` for a stable long-running planner.
3. External protocol failed tool/subagent paths. Status: complete.
   - `scripts/protocol-compat.ps1` covers denied tool events/projections and failed `runtime_process` subagent events/timeline over the public WebSocket/API surface.
4. Process leak guard. Status: complete.
   - Gateway-backed Playwright helper snapshots Gateway/Agent processes before each test and fails teardown if newly created processes remain after cleanup.

Exit criteria:

- All listed items have tests at the correct layer.
- `docs/10-development-status.md` no longer lists generic failure-path coverage as a near-term unknown.

Status: complete. M2 is also complete; the active milestone is M3 memory and context.

### M2: Session and History Foundation

Goal: prepare for memory and compaction without rewriting the UI later.

Status: complete. Session fork/compact design, Gateway backend, Desktop controls, Gateway-backed E2E, and protocol compatibility coverage are implemented.

Work items, in order:

1. Define session fork/compact domain model.
2. Add Gateway APIs for session fork and compact preview.
3. Add persisted compaction records or summaries.
4. Add Desktop affordances in Activity/session sidebar.
5. Add E2E for fork/compact restore.

Do not start new M2 work unless fork/compact regressions appear.

Exit criteria:

- A session can be forked without corrupting history.
- A compacted session has a traceable source and summary.
- Desktop restore handles forked/compacted sessions.

### M3: Memory and Context

Goal: add useful long-running agent memory while preserving auditability.

Status: complete for backend, Runtime injection, Desktop inspection/delete UI, and Gateway-mediated Runtime `memory.*` tools.

Work items, in order:

1. Define memory record types and retention policy.
2. Add Gateway persistence and APIs. Status: complete.
3. Add Runtime context injection contract. Status: complete.
4. Add Desktop memory inspection and delete controls. Status: complete.
5. Add tests for memory creation, retrieval, deletion, and replay impact. Status: complete for Gateway, Runtime injection, and Desktop workflow.

Guardrails:

- Memory must be inspectable.
- Memory must not silently override user-provided workspace context.
- Memory changes must be linked to a session/run.

### M4: MCP stdio Tools

Goal: support external MCP-style stdio tools without compromising the existing Runtime stdio JSON-RPC discipline.

Work items, in order:

1. Define MCP tool process model separately from Agent Runtime process model. Status: complete in `docs/19-mcp-stdio-tools-design.md`.
2. Add tool registry and capability discovery.
3. Add permission and risk mapping.
4. Add execution, timeout, and process cleanup.
5. Add Desktop tool audit display.
6. Add protocol and E2E tests.

Guardrails:

- Agent Runtime stdout remains JSON-RPC only.
- MCP logs must not pollute protocol stdout.
- High-risk MCP tools must use the same permission path as built-in tools.

### M5: External Access and Hardening

Goal: make WebSocket/HTTP usable outside the local Desktop without weakening local safety.

Work items:

1. Token scopes and auth policy.
2. Remote access mode.
3. Rate limits and request size limits.
4. Audit log export.
5. Crash reporting and process supervision.

This milestone should not start before M1 is closed.

## 5. Verification Tiers

Use tiers to avoid over-testing every small edit.

| Tier | When to run | Commands |
| --- | --- | --- |
| T0 focused | Single-file logic/test edit | Relevant `go test` package, `npm test`, or specific Playwright grep |
| T1 module | Feature in one layer | `go test ./modules/<module>/...` or `npm run test:ui -- --grep ...` |
| T2 integration | WebSocket/Gateway/Desktop behavior | `npm run test:ui -- --grep @gateway-backed`, `scripts/protocol-compat.ps1` |
| T3 release gate | Before claiming milestone complete | Full Go tests, frontend unit/UI/build, protocol compat, Wails build, optional full smoke |

Current full gate:

```powershell
go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/... ./modules/desktop/... ./modules/cli/...
cd modules\desktop\frontend
npm test
npm run test:ui
npm run build
cd ..\..\
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1
cd modules\desktop
wails3 build
```

## 6. Definition of Done

A task is done only when:

1. The behavior is implemented.
2. The correct verification layer covers it.
3. Existing tests still pass at the appropriate tier.
4. User-visible behavior is reflected in Desktop if it affects users.
5. Protocol changes are documented in the relevant protocol/design doc.
6. `docs/10-development-status.md` is updated if project state changes.
7. Temporary Playwright `test-results` are cleaned after local runs.

## 7. Current Next Queue

Next work should be picked from this queue, top first:

1. Session fork/compact design document update. Status: complete.
   - Design: `docs/13-session-fork-compact-design.md`.
2. Session fork/compact backend. Status: complete.
   - Gateway models, migrations, APIs, service tests, and protocol compatibility coverage are implemented.
3. Session fork/compact Desktop affordances and Gateway-backed E2E. Status: complete.
4. Memory/history design. Status: complete.
   - Design: `docs/14-memory-history-design.md`.
5. Memory/history backend. Status: complete.
6. Runtime memory injection. Status: complete.
7. Desktop Memory UI. Status: complete.
8. Optional `memory.*` Runtime tools. Status: complete.
9. MCP stdio tools design. Status: complete for v0.1.1.

Do not start MCP implementation before items 1-6 are complete or explicitly deferred in this document.

For v0.1.1, item 9 is complete. MCP implementation remains blocked until a new implementation slice is explicitly opened.

The active owner/work-lane split and stop rules for this queue are tracked in `docs/12-current-execution-plan.md`.

## 8. Documentation Ownership

Use these docs for different levels:

- `docs/10-development-status.md`: factual current state.
- `docs/11-structured-development-roadmap.md`: next-order control and governance.
- `docs/08-development-plan.md`: historical MVP plan and validation matrix.
- `docs/05-stdio-jsonrpc-multiplexing.md`: Runtime/Gateway protocol details.
- `docs/06-desktop-gateway-integration.md`: Desktop/Gateway protocol and UI integration.

When a major milestone changes, update both:

1. `docs/10-development-status.md`
2. `docs/11-structured-development-roadmap.md`
