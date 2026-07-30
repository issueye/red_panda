# red_panda documentation

## Current truth

| Doc | Purpose |
| --- | --- |
| [10-development-status.md](10-development-status.md) | What is implemented now |
| [35-redundancy-convergence-checklist.md](35-redundancy-convergence-checklist.md) | Wave 0–5 convergence backlog (mostly done) |
| [36-optimization-plan.md](36-optimization-plan.md) | **System optimization plan** (Goal correctness → Runtime split → Frontend → MCP/CLI/CI) |
| [37-worker-pool-v0.2.0-refactor-design.md](37-worker-pool-v0.2.0-refactor-design.md) | **v0.2.0 breaking WorkerPool architecture proposal** |
| [38-code-directness-optimization-plan.md](38-code-directness-optimization-plan.md) | **Active code-directness plan** (parallel coordinator simplification and compatibility cleanup) |
| [39-provider-runtime-decoupling-plan.md](39-provider-runtime-decoupling-plan.md) | **Provider-Runtime decoupling plan** (injection, provider split, neutral contract follow-up) |
| [40-centralization-abstraction-plan.md](40-centralization-abstraction-plan.md) | **Active centralization plan** (provider-neutral requests, RunStateStore, Settings resources) |
| [41-redundancy-overimpl-optimization-plan.md](41-redundancy-overimpl-optimization-plan.md) | **Active redundancy / over-implementation plan** (default tool surface, state-tool pipeline, narrative) |
| [42-robotgo-flow-mcp-skill.md](42-robotgo-flow-mcp-skill.md) | robotgo-flow MCP server + native skill integration |
| [43-scheduled-task-design.md](43-scheduled-task-design.md) | **Scheduled tasks design** (Gateway scheduler → run.start) |
| [44-v0.2.1-development-plan.md](44-v0.2.1-development-plan.md) | **v0.2.1 plan** — scheduled tasks delivery slices |
| [45-session-system-abstraction-design.md](45-session-system-abstraction-design.md) | **Session system abstraction & optimization** |
| [46-session-store-development-plan.md](46-session-store-development-plan.md) | **Session store, context, summary, and lifecycle implementation plan** |
| [47-complexity-redundancy-development-plan.md](47-complexity-redundancy-development-plan.md) | **Complexity & redundancy wave** (Goal loop pure decisions, state-tool boilerplate, alias cleanup) |
| [48-session-correctness-optimization-plan.md](48-session-correctness-optimization-plan.md) | **Session correctness P0** (compact mutex, preserveLive merge, hydrate generation) |
| [49-session-hard-delete-jsonl-archive.md](49-session-hard-delete-jsonl-archive.md) | **Session hard-delete + JSONL archive** (replace soft-delete) |
| [50-session-service-decomposition.md](50-session-service-decomposition.md) | **SessionService decomposition** (extract ContextPacker/Compactor/PurgeService) |
| [51-image-multimodal-design.md](51-image-multimodal-design.md) | **Image / multimodal attachments** (Implemented: upload, image_ref, vision, fork/GC) |
| [52-image-multimodal-development-plan.md](52-image-multimodal-development-plan.md) | **Image / multimodal implementation plan** (Slice A–D delivered) |
| [53-plugin-system-refactor-design.md](53-plugin-system-refactor-design.md) | **v0.3.0 Plugin Architecture** (Breaking: registry/hook/plugin refactoring; P1–P4 plugin model) |
| [54-plugin-system-development-plan.md](54-plugin-system-development-plan.md) | **v0.3.0 Plugin Development Plan** (Phase 1–4, 4 Phases, T1–T12, parallel execution) |
| [plans/2026-07-19-convergence-wave.md](plans/2026-07-19-convergence-wave.md) | **Convergence wave** (doc/UI sync, Session service decomposition, large-file splits) |
| [02-functional-design.md](02-functional-design.md) | Product/architecture goals |
| [03-gateway-mvc-design.md](03-gateway-mvc-design.md) | Gateway layering |
| [04-agent-runtime-design.md](04-agent-runtime-design.md) | Runtime / v0.1 subagent model (superseded for v0.2.0 by doc 37) |
| [05-stdio-jsonrpc-multiplexing.md](05-stdio-jsonrpc-multiplexing.md) | Gateway↔Runtime JSON-RPC (IPC default; stdio legacy) |
| [06-desktop-gateway-integration.md](06-desktop-gateway-integration.md) | Desktop HTTP/WS contract |
| [07-desktop-tech-ui-design.md](07-desktop-tech-ui-design.md) | Desktop stack & UI IA |

## Feature design (active domains)

| Doc | Domain |
| --- | --- |
| [13-session-fork-compact-design.md](13-session-fork-compact-design.md) | Session fork / compact |
| [45-session-system-abstraction-design.md](45-session-system-abstraction-design.md) | Session aggregate abstraction + lifecycle optimization |
| [46-session-store-development-plan.md](46-session-store-development-plan.md) | Session persistence projection, context state, summary history, and deletion lifecycle |
| [14-memory-history-design.md](14-memory-history-design.md) | Memory |
| [15-memory-runtime-tools-design.md](15-memory-runtime-tools-design.md) | Memory tools |
| [19-mcp-stdio-tools-design.md](19-mcp-stdio-tools-design.md) | MCP (config + discovery + tools/call MVP) |
| [21-ui-normalization.md](21-ui-normalization.md) | UI primitives |
| [27-desktop-ued-specification.md](27-desktop-ued-specification.md) | UED |
| [28-desktop-ued-audit.md](28-desktop-ued-audit.md) | UED audit |
| [30-todo-feature-design.md](30-todo-feature-design.md) | Todo checklist |
| [43-scheduled-task-design.md](43-scheduled-task-design.md) | Scheduled tasks (cron / interval / one-shot) |
| [44-v0.2.1-development-plan.md](44-v0.2.1-development-plan.md) | v0.2.1 development plan |
| [51-image-multimodal-design.md](51-image-multimodal-design.md) | Image / multimodal chat attachments |
| [33-agent-management.md](33-agent-management.md) | v0.1 Agent definitions (renamed WorkerProfile in v0.2.0) |
| [37-worker-pool-v0.2.0-refactor-design.md](37-worker-pool-v0.2.0-refactor-design.md) | WorkerPool / Assignment / Mailbox refactor |

## Background

| Doc | Purpose |
| --- | --- |
| [01-existing-project-analysis.md](01-existing-project-analysis.md) | Early analysis of related projects |

## Archive

Historical roadmaps, parallel plans, and version release notes live under [`archive/`](archive/). They are kept for audit history and are **not** the current source of truth.
