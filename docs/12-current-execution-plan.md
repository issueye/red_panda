# Current Execution Plan

Updated: 2026-07-09

This document is the active execution board for the next development slice. It keeps `red_panda` work ordered, reviewable, and safe for parallel workers.

Release boundary: `docs/16-v0.1.0-release-plan.md` defines what must be finished before v0.1.0 and what is deferred.

## 1. Current Development Snapshot

`red_panda` has a working MVP three-layer loop:

```text
Desktop(Wails v3 + React) <-> Gateway(Gin/GORM/SQLite no-cgo) <-> Agent Runtime(stdio JSON-RPC)
```

Current state by layer:

| Layer | Current state | Stability | Next concern |
| --- | --- | --- | --- |
| Protocol | JSON-RPC, WebSocket envelopes, `root_seq` replay/resume, permission/tool/subagent events, session fork/compact coverage, memory CRUD/preview HTTP compatibility coverage, `memory_injected` event, internal `memory.tool.execute`. | Stable MVP | Define MCP/tool expansion before implementation. |
| Agent Runtime | Echo/OpenAI-compatible providers, tools, permissions, cancellation, in-process/process/process-pool subagents, memory context injection, Gateway-mediated `memory.*` tools. | Stable MVP | MCP/tool expansion needs a scoped design before implementation. |
| Gateway | Gin MVC, SQLite persistence, projections, WebSocket routing, provider profiles, per-run runtime mode, session fork/compact APIs, memory CRUD/preview APIs, runtime-originated memory tool handler. | Stable MVP | MCP/tool expansion needs a scoped design before implementation. |
| Desktop | Chat, tool cards, permission cards, subagents, settings, activity timeline, reconnect/resume, fork/compact controls, Memory tab. | Stable MVP | No broad UI redesign; only add UI when a verified backend/tool contract exists. |
| Persistence | Sessions, messages, runs, events, tools, permissions, provider profiles, session lineage, compactions, memory records. | Stable MVP | None for the immediate decision/design slice. |
| Verification | Go tests, frontend unit tests, Playwright fixtures, Gateway-backed E2E, protocol script, Wails build. | Good | Keep tiered gates; avoid full test matrix for every small edit. |

## 2. Active Rule

M1 failure-path closure, M2 session fork/compact, Gateway memory backend, Runtime memory injection, Desktop Memory UI, and Gateway-mediated Runtime `memory.*` tools are complete.

The decision gate is closed. The selected `memory.*` Runtime tool slice from `docs/15-memory-runtime-tools-design.md` is implemented and verified at Go/protocol-compat level.

This slice must stay smaller than MCP:

1. Runtime exposes `memory.list/create/update/delete` tool definitions.
2. Runtime keeps tool policy and permission gating.
3. Gateway executes memory persistence through an internal stdio JSON-RPC request from Runtime.
4. Desktop observes results through existing tool audit, Activity, and Memory tab.

Do not start MCP implementation, plugin work, or broad UI redesign before the v0.1.0 release gate is finished.

## 3. Active Queue

The `memory.*` slice is complete. Remaining v0.1.0 work is release stabilization only.

| Order | Task | Scope | Done when |
| --- | --- | --- | --- |
| 1 | Protocol DTOs | `modules/protocol/methods` | Complete. `memory.tool.execute` method and request/result DTOs exist. |
| 2 | Gateway runtime-client handler | `modules/gateway/internal/gateway/infra/runtimeclient`, `service` | Complete. Gateway handles Runtime-originated memory tool requests and calls `MemoryService`. |
| 3 | Runtime memory tool definitions | `modules/agent/internal/runtime` | Complete. `memory.list/create/update/delete` appear in available tools. |
| 4 | Runtime memory tool execution | `modules/agent/internal/runtime` | Complete. Runtime calls Gateway-mediated executor while preserving tool events and permission flow. |
| 5 | Tests | Agent/Gateway/Protocol tests | Complete. Runtime, Gateway service, and runtimeclient tests cover the new path. |
| 6 | Protocol compatibility | `scripts/protocol-compat.ps1` | Complete. External run path proves memory create/list/deny through normal tool audit. |
| 7 | Status update | `docs/*`, `README.md` | Complete. Status reflects completed `memory.*` tools and v0.1.0 release gate. |

No MCP implementation should start until this queue is complete or explicitly deferred.

## 4. Parallel Worker Plan

Parallel work is allowed only for read-only investigation or disjoint docs.

| Worker | Lane | Allowed files now | Current assignment |
| --- | --- | --- | --- |
| Worker A | Protocol/Gateway bridge | `modules/protocol/methods`, `modules/gateway/internal/gateway/infra/runtimeclient`, focused Gateway tests | Add internal memory tool RPC dispatch. |
| Worker B | Runtime tools | `modules/agent/internal/runtime`, focused Runtime tests | Add tool definitions and executor callback. |
| Worker C | Protocol compatibility | `scripts/protocol-compat.ps1` | Add run-level memory tool coverage after A/B land. |
| Worker D | Documentation | `docs/*`, `README.md` | Keep execution board/status accurate after facts are verified. |

Do not let two workers edit these files at the same time:

- `docs/10-development-status.md`
- `docs/11-structured-development-roadmap.md`
- `docs/12-current-execution-plan.md`
- `README.md`
- `modules/protocol/methods/methods.go`
- `modules/gateway/internal/gateway/infra/runtimeclient/client.go`
- `modules/agent/internal/runtime/tools.go`
- `modules/agent/internal/runtime/runtime.go`

## 5. Verification Gates

Use the smallest gate that proves the current change:

| Gate | Use for | Commands |
| --- | --- | --- |
| T0 | Focused Runtime/Gateway helper | Relevant package tests |
| T1 | Runtime/Gateway tool slice | `go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...` |
| T2 | External behavior | `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1` and targeted Gateway-backed UI if user-visible. |
| T3 | Milestone completion | Full Go tests, frontend unit/UI/build, protocol compatibility, Wails build |

## 6. Stop Rules

Pause and reassess before coding if any of these happen:

- `memory.*` tools write memory without the existing permission/audit policy.
- Agent Runtime stdout could include non-JSON-RPC MCP logs.
- New tool behavior bypasses Gateway persistence or Desktop inspectability.
- A test requires arbitrary sleeps instead of waiting for real Gateway/API/UI state.

## 7. Immediate Next Implementation

Release stabilization is complete for v0.1.0.

Next controlled work:

1. Write an MCP stdio tools design document.
2. Keep MCP implementation deferred until that design is reviewed.
3. Do not broaden Desktop UI or plugin scope during the design slice.
