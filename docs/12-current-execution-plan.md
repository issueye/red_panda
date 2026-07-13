# Current Execution Plan

Updated: 2026-07-14

This document is the active execution board for the next development slice. It keeps `red_panda` work ordered, reviewable, and safe for parallel workers.

Current release baseline:

- v0.1.0 is release-gate complete.
- Commit: `e223831`
- Tag: `v0.1.0`
- v0.1.1 planning source: `docs/18-v0.1.1-development-plan.md`
- v0.1.2 planning source: `docs/22-v0.1.2-development-plan.md`
- v0.1.3 planning source: `docs/24-v0.1.3-development-plan.md`
- v0.1.4 planning source: `docs/25-v0.1.4-development-plan.md`
- v0.1.5 planning source: `docs/26-v0.1.5-development-plan.md`

## 1. Current Development Snapshot

`red_panda` has a stable v0.1.0 three-layer loop:

```text
Desktop(Wails v3 + React) <-> Gateway(Gin/GORM/SQLite no-cgo) <-> Agent Runtime(stdio JSON-RPC)
```

Current state by layer:

| Layer | Current state | Stability | Next concern |
| --- | --- | --- | --- |
| Protocol | Existing run/memory contracts plus MCP config/discovery DTOs and managed skill summary/detail/mutate/delete DTOs and method constants. | Read-only discovery + skill management contracts complete | MCP call contracts remain. |
| Agent Runtime | Ordered adjacent-worker scheduling, FIFO process pool, bounded cancellation, health checks, hard worker limits, Agent-definition enforcement, plus existing MCP discovery and managed skills. | Worker optimization gates passed | MCP tool registration and `tools/call` are not implemented. |
| Gateway | Authoritative root/worker/provider budgets, stale-run reconciliation, Runtime-exit terminalization, Agent snapshots, plus existing MCP config/discovery and skill management. | Resource governance and recovery complete | Gateway does not execute provider-facing MCP tools. |
| Desktop | MCP configuration management, read-only discovered tool visibility, and a Gateway-backed Skills tab (list/create/edit/delete). | v0.1.5 UI + Skills tab complete | No callable MCP tools. |
| Verification | All Go modules, Runtime/Gateway race checks, 97 frontend unit tests, 26 Playwright tests, production build, protocol compatibility, and WebSocket smoke pass. | Worker optimization and prior discovery/skills gates covered | Executable MCP gates remain future work. |

## 2. Active Rule

M0 stabilization, read-only MCP discovery, managed skills, and all six worker-mode optimization phases are implemented. The next controlled slice may build on the now-bounded worker baseline without weakening its ordering, admission, recovery, or cancellation invariants.

Allowed now:

1. Preserve worker scheduling, budget, recovery, cancellation, and Agent-definition regression coverage.
2. Design and implement the next MCP registry/permission slice behind the existing global budgets.
3. Design the later MCP registry and permission integration without enabling `tools/call` yet.
4. Polish managed skills UX (e.g. validation feedback) without adding automatic selection.

Not allowed now:

1. Provider-facing MCP tool registration or execution.
2. Plugin marketplace work.
3. Remote access hardening.
4. Broad Desktop UI redesign.
5. MCP `tools/call` or provider-facing MCP tool execution.
6. MCP permission/risk integration before discovery is stable.
7. Unbounded or client-controlled process/provider concurrency.

## 3. Active Queue

Only the following tasks are active, in this order.

| Order | Task | Scope | Done when |
| --- | --- | --- | --- |
| 1 | Gateway session handoff | `modules/gateway/internal/gateway/service` | Persisted conversation is ordered and excludes the current input. Status: implemented. |
| 2 | Provider context and tools | `modules/agent/internal/runtime/provider.go` | History is sent to the provider and tools remain available after results. Status: implemented. |
| 3 | Chained tool regression | `modules/agent/internal/runtime` tests | Two sequential tools execute and the run finishes completed. Status: implemented. |
| 4 | M0 full verification | Go, frontend, protocol compatibility | Status: passed. |
| 5 | Protocol MCP config DTOs | `modules/protocol` | Status: implemented and tested. |
| 6 | Gateway MCP config CRUD | Gateway model/repository/service/controller | Status: implemented without executing commands. |
| 7 | Desktop MCP config management | Desktop DTO/App/Settings/UI tests | Status: implemented against Gateway APIs. |
| 8 | Configuration slice integration gate | Go/frontend/protocol compatibility | Status: passed, including Wails build and real-provider smoke. |
| 9 | MCP read-only discovery | v0.1.5 scope | Status: implemented and verified. |
| 10 | Managed skills discovery + management | Protocol/Runtime/Gateway/Desktop | Status: implemented and verified (Slices 1–4, `docs/29-skills-development-plan.md`). Includes Gateway Runtime request-context detachment fix with regression test. |
| 11 | Worker-mode optimization | Runtime/Gateway/Desktop/Protocol | Status: implemented and fully verified (`docs/35-worker-mode-optimization-plan.md`). |
| 12 | Executable MCP registry and permission design | Cross-layer design | Status: next controlled slice; implementation must retain global budgets and worker lifecycle invariants. |

## 4. Parallel Worker Plan

Parallel work is allowed only when write scopes are disjoint.

| Worker | Lane | Allowed files now | Current assignment |
| --- | --- | --- | --- |
| Worker A | Protocol/config DTO | `modules/protocol`, focused tests | Completed MCP config DTO and timeout contract. |
| Worker B | Gateway config backend | Gateway model/repository/service/controller | Completed persistence, validation, redaction, and CRUD. |
| Worker C | Desktop config UI | Desktop frontend and tests | Completed Gateway-backed MCP settings workflow. |
| Root | Integration/docs | Cross-lane verification and status docs | Review combined gate and maintain execution boundary. |

Do not let two workers edit these files at the same time:

- `docs/10-development-status.md`
- `docs/11-structured-development-roadmap.md`
- `docs/12-current-execution-plan.md`
- `docs/18-v0.1.1-development-plan.md`
- `docs/19-mcp-stdio-tools-design.md`
- `docs/20-v0.1.1-release-notes.md`
- `docs/22-v0.1.2-development-plan.md`
- `docs/23-v0.1.2-release-notes.md`
- `README.md`

## 5. Verification Gates

Use the smallest gate that proves the current change:

| Gate | Use for | Commands |
| --- | --- | --- |
| T0 docs | Documentation-only MCP status changes | `rg "MCP stdio tools are implemented|MCP implementation is complete" docs README.md` should return no false claims. |
| T1 focused code | MCP config DTO/backend/frontend edit | Relevant Go package or frontend unit suite. |
| T2 config integration | MCP CRUD and Desktop management | Protocol/Gateway Go tests, frontend unit/UI suites. |
| T3 protocol | Public MCP config API | `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1`. |
| T4 release gate | Before v0.1.5 starts | T2/T3 plus frontend and Wails builds. |
| T5 worker baseline | Any Runtime/Gateway concurrency or process change | All Go modules, Runtime/Gateway race checks, frontend unit/build, full Playwright, protocol compatibility, WebSocket smoke, process-leak audit. |

## 6. Stop Rules

Pause and reassess before coding if any of these happen:

- Gateway code would start MCP commands, parse MCP stdout, or call MCP tools.
- MCP config validation would execute configured commands.
- `tools/call` or provider-facing MCP execution appears before read-only discovery is stable.
- MCP tools bypass the existing permission and audit path.
- Tests rely on arbitrary sleeps instead of concrete process/API state.

## 7. Immediate Next Action

The managed skills surface and all worker optimization phases are complete and verified through the audited full gate on 2026-07-14. The next controlled slice is executable MCP registry and permission design on top of the bounded worker baseline.

Next controlled work:

1. Preserve ordered worker batches and Gateway-owned root/worker/provider budgets.
2. Add MCP registry and permission contracts before enabling provider-facing `tools/call`.
3. Route any later MCP execution through existing cancellation, audit, and global resource controls.
4. Automatic managed-skill selection remains out of scope (explicit `skill.run` / management APIs only).
