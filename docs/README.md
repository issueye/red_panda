# red_panda documentation

## Current truth

| Doc | Purpose |
| --- | --- |
| [10-development-status.md](10-development-status.md) | What is implemented now |
| [35-redundancy-convergence-checklist.md](35-redundancy-convergence-checklist.md) | Wave 0–5 convergence backlog (mostly done) |
| [36-optimization-plan.md](36-optimization-plan.md) | **System optimization plan** (Goal correctness → Runtime split → Frontend → MCP/CLI/CI) |
| [37-worker-pool-v0.2.0-refactor-design.md](37-worker-pool-v0.2.0-refactor-design.md) | **v0.2.0 breaking WorkerPool architecture proposal** |
| [34-goal-implementation-optimization-plan.md](34-goal-implementation-optimization-plan.md) | Goal state-machine invariants (Track A detail) |
| [02-functional-design.md](02-functional-design.md) | Product/architecture goals |
| [03-gateway-mvc-design.md](03-gateway-mvc-design.md) | Gateway layering |
| [04-agent-runtime-design.md](04-agent-runtime-design.md) | Runtime / v0.1 subagent model (superseded for v0.2.0 by doc 37) |
| [05-stdio-jsonrpc-multiplexing.md](05-stdio-jsonrpc-multiplexing.md) | Gateway↔Runtime protocol |
| [06-desktop-gateway-integration.md](06-desktop-gateway-integration.md) | Desktop HTTP/WS contract |
| [07-desktop-tech-ui-design.md](07-desktop-tech-ui-design.md) | Desktop stack & UI IA |

## Feature design (active domains)

| Doc | Domain |
| --- | --- |
| [13-session-fork-compact-design.md](13-session-fork-compact-design.md) | Session fork / compact |
| [14-memory-history-design.md](14-memory-history-design.md) | Memory |
| [15-memory-runtime-tools-design.md](15-memory-runtime-tools-design.md) | Memory tools |
| [19-mcp-stdio-tools-design.md](19-mcp-stdio-tools-design.md) | MCP (config + discovery) |
| [21-ui-normalization.md](21-ui-normalization.md) | UI primitives |
| [27-desktop-ued-specification.md](27-desktop-ued-specification.md) | UED |
| [28-desktop-ued-audit.md](28-desktop-ued-audit.md) | UED audit |
| [30-todo-feature-design.md](30-todo-feature-design.md) | Todo checklist |
| [31-goal-loop-design.md](31-goal-loop-design.md) | Goal loop KD |
| [32-goal-loop-development-design.md](32-goal-loop-development-design.md) | Goal implementation |
| [33-agent-management.md](33-agent-management.md) | v0.1 Agent definitions (renamed WorkerProfile in v0.2.0) |
| [37-worker-pool-v0.2.0-refactor-design.md](37-worker-pool-v0.2.0-refactor-design.md) | WorkerPool / Assignment / Mailbox refactor |
| [34-goal-implementation-optimization-plan.md](34-goal-implementation-optimization-plan.md) | Goal correctness plan |

## Background

| Doc | Purpose |
| --- | --- |
| [01-existing-project-analysis.md](01-existing-project-analysis.md) | Early analysis of related projects |

## Archive

Historical roadmaps, parallel plans, and version release notes live under [`archive/`](archive/). They are kept for audit history and are **not** the current source of truth.
