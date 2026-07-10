# Current Execution Plan

Updated: 2026-07-10

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
| Protocol | Existing run/memory contracts plus MCP server config, timeout, and CRUD DTOs. | MCP config complete | MCP initialize/discovery/call contracts remain. |
| Agent Runtime | Ordered session context, sequential tool rounds, built-in/memory tools, permissions, cancellation, and subagents. | M0 gate passed | MCP process lifecycle, `tools/list`, and `tools/call` are not implemented. |
| Gateway | Existing APIs plus validated, persisted, redacted MCP server config CRUD under `/api/v1/mcp/servers`. | MCP config complete | Gateway does not start or call MCP servers. |
| Desktop | Existing workbench plus Gateway-backed MCP list/create/edit/enable-disable/delete management in Settings. | MCP config UI complete | No live MCP discovery, process state, or callable MCP tools. |
| Verification | MCP protocol DTO tests, Gateway repository/service/controller coverage, frontend DTO/UI coverage, and protocol CRUD compatibility are present. | Configuration slices covered | Runtime MCP process and IO-isolation gates remain future work. |

## 2. Active Rule

M0 stabilization, v0.1.2 MCP config CRUD, and v0.1.4 Desktop MCP config management are implemented. The next controlled slice is v0.1.5 read-only startup and discovery.

Allowed now:

1. Final integration verification and release status synchronization for the configuration slices.
2. v0.1.5 design-conformant MCP process supervisor work.
3. MCP initialize and read-only `tools/list` discovery with deterministic cleanup.
4. Fake-server tests for stdout/stderr isolation, timeout, and process-leak behavior.

Not allowed now:

1. Runtime MCP stdio process implementation.
2. Plugin marketplace work.
3. Remote access hardening.
4. Broad Desktop UI redesign.
5. MCP `tools/call` or provider-facing MCP tool execution.
6. MCP permission/risk integration before discovery is stable.
7. Automatic crash restart policy before startup and cleanup are proven.

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
| 9 | MCP read-only discovery | v0.1.5 scope | Start only after the configuration gate is accepted. |

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

## 6. Stop Rules

Pause and reassess before coding if any of these happen:

- Gateway code would start MCP commands, parse MCP stdout, or call MCP tools.
- MCP config validation would execute configured commands.
- `tools/call` or provider-facing MCP execution appears before read-only discovery is stable.
- MCP tools bypass the existing permission and audit path.
- Tests rely on arbitrary sleeps instead of concrete process/API state.

## 7. Immediate Next Action

The configuration surface and its integration gate are complete. The immediate action is to begin only the v0.1.5 read-only discovery scope.

Next controlled work:

1. Keep Gateway free of MCP process execution.
2. Start v0.1.5 with controlled process startup, initialize, and read-only `tools/list` only.
3. Add fake-server coverage for stdout/stderr isolation, timeout, cleanup, and process leaks.
4. Keep `tools/call`, provider-facing execution, permission integration, and restart policy for later verified slices.
