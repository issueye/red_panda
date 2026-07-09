# Current Execution Plan

Updated: 2026-07-09

This document is the active execution board for the next development slice. It keeps `red_panda` work ordered, reviewable, and safe for parallel workers.

Current release baseline:

- v0.1.0 is release-gate complete.
- Commit: `e223831`
- Tag: `v0.1.0`
- v0.1.1 planning source: `docs/18-v0.1.1-development-plan.md`

## 1. Current Development Snapshot

`red_panda` has a stable v0.1.0 three-layer loop:

```text
Desktop(Wails v3 + React) <-> Gateway(Gin/GORM/SQLite no-cgo) <-> Agent Runtime(stdio JSON-RPC)
```

Current state by layer:

| Layer | Current state | Stability | v0.1.1 concern |
| --- | --- | --- | --- |
| Protocol | JSON-RPC, WebSocket envelopes, `root_seq` replay/resume, permission/tool/subagent events, session fork/compact, memory CRUD/preview, `memory_injected`, internal `memory.tool.execute`. | v0.1.0 stable | MCP protocol/config contracts are not designed. |
| Agent Runtime | Echo/OpenAI-compatible providers, built-in tools, permissions, cancellation, in-process/process/process-pool subagents, memory injection, Gateway-mediated `memory.*` tools. | v0.1.0 stable | MCP stdio process ownership, IO isolation, registry, timeout, and cancel need design. |
| Gateway | Gin MVC, SQLite persistence, projections, WebSocket routing, provider profiles, per-run runtime mode, fork/compact APIs, memory APIs, runtime-originated memory tool handler. | v0.1.0 stable | MCP config persistence and Runtime handoff need design. |
| Desktop | Chat, tool cards, permission cards, subagents, settings, Activity timeline, reconnect/resume, fork/compact, Memory tab. | v0.1.0 stable | No MCP UI until backend contract is designed. |
| Verification | Go tests, frontend unit tests, Playwright fixtures, serialized Gateway-backed E2E, protocol script, Wails build. | v0.1.0 gate passed | Keep test tiers stable while designing MCP. |

## 2. Active Rule

v0.1.1 is a stabilization and design slice.

Allowed now:

1. MCP stdio tools design.
2. Test harness design for fake MCP servers.
3. Release hygiene documentation.
4. Small bug fixes only if needed to keep v0.1.0 reproducible.

Not allowed now:

1. MCP implementation.
2. Plugin marketplace work.
3. Remote access hardening.
4. Broad Desktop UI redesign.
5. Refactoring ToolRunner before the MCP design is accepted.

## 3. Active Queue

Only the following tasks are active, in this order.

| Order | Task | Scope | Done when |
| --- | --- | --- | --- |
| 1 | MCP stdio design | `docs/19-mcp-stdio-tools-design.md` | Complete. Process ownership, config contract, lifecycle, tool registry, permissions, IO isolation, events, and tests are documented. |
| 2 | Fake MCP test matrix | Design doc | Complete. Matrix covers valid JSON-RPC, stdout garbage, stderr logs, timeout, cancel, crash, and large output. |
| 3 | Post-design implementation backlog | `docs/12-current-execution-plan.md`, `docs/11-structured-development-roadmap.md` | Complete. MCP work is split into small slices; implementation remains gated. |
| 4 | v0.1.1 release notes draft | `docs/20-v0.1.1-release-notes.md` | Complete. Notes explain v0.1.1 as stabilization/design, not MCP implementation. |
| 5 | Status sync | `docs/10-development-status.md`, `README.md` | Complete. Docs agree on v0.1.1 scope and current status. |

## 4. Parallel Worker Plan

Parallel work is allowed only for read-only analysis or disjoint docs.

| Worker | Lane | Allowed files now | Current assignment |
| --- | --- | --- | --- |
| Worker A | Runtime MCP boundary | `docs/19-mcp-stdio-tools-design.md`, read-only `modules/agent` | Document process lifecycle, stdout/stderr isolation, registry, timeout, and cancel. |
| Worker B | Gateway MCP config | `docs/19-mcp-stdio-tools-design.md`, read-only `modules/gateway` | Document config persistence/API and Gateway-to-Runtime handoff. |
| Worker C | Verification | `docs/19-mcp-stdio-tools-design.md`, `docs/12-current-execution-plan.md` | Define fake server matrix and v0.1.1 gates. |
| Worker D | Release docs | `docs/*`, `README.md` | Keep status, roadmap, and notes synchronized. |

Do not let two workers edit these files at the same time:

- `docs/10-development-status.md`
- `docs/11-structured-development-roadmap.md`
- `docs/12-current-execution-plan.md`
- `docs/18-v0.1.1-development-plan.md`
- `docs/19-mcp-stdio-tools-design.md`
- `docs/20-v0.1.1-release-notes.md`
- `README.md`

## 5. Verification Gates

Use the smallest gate that proves the current change:

| Gate | Use for | Commands |
| --- | --- | --- |
| T0 docs | Documentation-only v0.1.1 changes | `rg "MCP stdio tools are implemented|MCP implementation is complete" docs README.md` should return no false claims. |
| T1 focused code | Small code fix during v0.1.1 | Relevant `go test` package or frontend unit test. |
| T2 integration | Any Gateway/Runtime/Desktop behavior change | `go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...` and `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1`. |
| T3 release gate | Before v0.1.1 release claim | Full v0.1.0 gate from `docs/18-v0.1.1-development-plan.md`. |

## 6. Stop Rules

Pause and reassess before coding if any of these happen:

- MCP design would place tool execution in Gateway instead of Runtime.
- MCP stdout/stderr could pollute Runtime stdout.
- MCP tools would bypass `EvaluateToolPolicy` or permission requests.
- MCP tool results would not appear in existing tool audit and Activity timeline.
- Desktop UI is proposed before MCP backend/tool contract is designed.
- Tests rely on arbitrary sleeps instead of concrete process/API state.

## 7. Immediate Next Action

v0.1.1 design tasks are complete.

Next controlled work:

1. Decide whether to release/tag v0.1.1 as a documentation/design release.
2. If continuing into implementation, open a new execution plan for MCP-1 only: protocol/config DTOs and Gateway config CRUD.
3. Keep Runtime MCP process implementation blocked until MCP-1 is complete and verified.
