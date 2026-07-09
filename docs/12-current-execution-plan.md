# Current Execution Plan

Updated: 2026-07-09

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

| Layer | Current state | Stability | v0.1.2 concern |
| --- | --- | --- | --- |
| Protocol | JSON-RPC, WebSocket envelopes, `root_seq` replay/resume, permission/tool/subagent events, session fork/compact, memory CRUD/preview, `memory_injected`, internal `memory.tool.execute`. | v0.1.1 stable | Add MCP server config DTOs only. |
| Agent Runtime | Echo/OpenAI-compatible providers, built-in tools, permissions, cancellation, in-process/process/process-pool subagents, memory injection, Gateway-mediated `memory.*` tools. | v0.1.1 stable | Runtime MCP process execution remains blocked. |
| Gateway | Gin MVC, SQLite persistence, projections, WebSocket routing, provider profiles, per-run runtime mode, fork/compact APIs, memory APIs, runtime-originated memory tool handler. | v0.1.1 stable | Add MCP config persistence, validation, and CRUD without executing commands. |
| Desktop | Chat, tool cards, permission cards, subagents, settings, Activity timeline, reconnect/resume, fork/compact, Memory tab, normalized UI primitives. | v0.1.1 stable | No MCP UI in v0.1.2. |
| Verification | Go tests, frontend unit tests, Playwright fixtures, serialized Gateway-backed E2E, protocol script, Wails build. | v0.1.1 gate passed | Add protocol compatibility for MCP config CRUD. |

## 2. Active Rule

v0.1.2 is the MCP-1 implementation slice: protocol/config DTOs and Gateway config CRUD only.

Allowed now:

1. MCP server config protocol DTOs.
2. Gateway MCP config persistence.
3. Gateway MCP config validation and CRUD APIs.
4. Protocol compatibility coverage for MCP config CRUD.
5. Release hygiene documentation.

Not allowed now:

1. Runtime MCP stdio process implementation.
2. Plugin marketplace work.
3. Remote access hardening.
4. Broad Desktop UI redesign.
5. Native Desktop MCP settings UI before backend contracts exist.
6. MCP `tools/list` or `tools/call`.
7. Refactoring ToolRunner before MCP config CRUD is complete.

## 3. Active Queue

Only the following tasks are active, in this order.

| Order | Task | Scope | Done when |
| --- | --- | --- | --- |
| 1 | Protocol MCP config DTOs | `modules/protocol` | MCP server config structs and JSON contracts are added and tested. |
| 2 | Gateway MCP config persistence | `modules/gateway/internal/gateway/model`, repository, migration | Config records persist, list, update, and delete without executing commands. |
| 3 | Gateway validation/service | `modules/gateway/internal/gateway/service` | Unsafe names, commands, timeouts, allowlists, and risk overrides are rejected. |
| 4 | Gateway HTTP CRUD | `modules/gateway/internal/gateway/controller`, routes | `/api/v1/mcp/servers` CRUD is implemented. |
| 5 | Protocol compatibility | `scripts/protocol-compat.ps1` | External API smoke covers MCP config CRUD. |
| 6 | Status sync | `docs/10-development-status.md`, `README.md`, `docs/23-v0.1.2-release-notes.md` | Docs state v0.1.2 is MCP config CRUD only. |

## 4. Parallel Worker Plan

Parallel work is allowed only when write scopes are disjoint.

| Worker | Lane | Allowed files now | Current assignment |
| --- | --- | --- | --- |
| Worker A | Protocol DTOs | `modules/protocol`, protocol tests | Add MCP server config DTOs and JSON/default tests. |
| Worker B | Gateway persistence/service | `modules/gateway/internal/gateway/model`, repository, service tests | Add persistence and validation for MCP server config without command execution. |
| Worker C | Gateway HTTP/API compatibility | `modules/gateway/internal/gateway/controller`, routes, `scripts/protocol-compat.ps1` | Add MCP config CRUD routes and external compatibility coverage. |
| Worker D | Release docs | `docs/*`, `README.md` | Keep v0.1.2 status, roadmap, and notes synchronized. |

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
| T0 docs | Documentation-only v0.1.2 changes | `rg "MCP stdio tools are implemented|MCP implementation is complete" docs README.md` should return no false claims. |
| T1 focused code | MCP DTO or validation edit | Relevant `go test` package. |
| T2 integration | MCP config CRUD behavior | `go test ./modules/protocol/... ./modules/gateway/...` and `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1`. |
| T3 release gate | Before v0.1.2 release claim | Full gate from `docs/22-v0.1.2-development-plan.md`. |

## 6. Stop Rules

Pause and reassess before coding if any of these happen:

- A v0.1.2 change would start or supervise MCP child processes.
- Gateway code would parse MCP stdout or call MCP tools.
- MCP config validation would execute configured commands.
- Runtime tool registry, `tools/list`, or `tools/call` work appears in this slice.
- Desktop MCP UI is proposed before Gateway config CRUD is stable.
- Tests rely on arbitrary sleeps instead of concrete process/API state.

## 7. Immediate Next Action

v0.1.2 is designated as MCP-1. v0.1.3/v0.1.4/v0.1.5 planning is now defined.

Next controlled work:

1. Implement protocol MCP config DTOs.
2. Implement Gateway MCP config persistence and CRUD APIs.
3. Add protocol compatibility coverage.
4. Keep Runtime MCP process implementation blocked until v0.1.2 is complete and verified.
5. Use v0.1.3 for Desktop clarity/localization stabilization work that does not alter backend contracts.
6. Start v0.1.4 Desktop MCP config UI only after v0.1.2 Gateway config CRUD is stable.
7. Start v0.1.5 MCP read-only discovery only after config UI is complete.
