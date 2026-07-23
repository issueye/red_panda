# Centralization and Abstraction Implementation Plan

Updated: 2026-07-23
Status: Wave 1 complete; provider-neutral request path closed via docs/39 Wave 2
Baseline: `225f14b`

## 1. Objective

Centralize stable concepts that are currently spread across layers while avoiding generic frameworks and domain-erasing abstractions.

This wave targets three independent high-value boundaries:

1. Runtime composes provider-neutral model requests; provider adapters only encode/decode vendor protocols.
2. Runtime-owned per-run mutable state has one lifecycle and cleanup owner.
3. Desktop Settings resources travel as domain objects instead of dozens of flat props.

## 2. Design Rules

- Preserve public JSON-RPC, HTTP, WebSocket, tool, and persistence contracts.
- Preserve OpenAI-compatible request body ordering and tool-call behavior.
- Preserve EchoProvider behavior and local test workflows.
- Preserve Run cancellation, Goal/Todo snapshots, event sequence monotonicity, and MCP cleanup.
- Preserve Settings UI behavior, labels, drafts, loading states, and CRUD side effects.
- Prefer explicit domain APIs over reflection, generic service locators, or universal CRUD frameworks.
- Do not mix Gateway StateToolDispatcher, repository generics, CSS redesign, or new product behavior into this wave.

## 3. Wave 1 Workstreams

### W1-A Provider-neutral request and PromptComposer

Primary ownership:

- `modules/agent/internal/provider/*`
- `modules/agent/internal/runtime/loop.go`
- a focused Runtime prompt/model request composer file
- focused Provider/Runtime tests

Target boundary:

```text
methods.ReplyParams
  -> Runtime PromptComposer
  -> provider.Request { Model, Messages, Tools, ToolRounds, Options }
  -> OpenAI-compatible adapter
```

Implementation:

1. Add provider-owned neutral `Message`, request options, and request types.
2. Move Goal/Todo/Worker/Memory/Skill/Specialist prompt ordering decisions out of provider serialization and into a Runtime-owned composer.
3. Keep vendor message/tool serialization in provider.
4. Remove provider production imports of broad `protocol/methods` types where feasible.
5. Preserve per-run provider override behavior through a narrow provider configuration field.
6. Preserve model-facing tool history and multi-tool round ordering.

Acceptance:

- provider request serialization no longer decides Runtime prompt policy;
- provider production code does not require `ReplySession`, `ReplyInput`, or `ReplyOptions`;
- golden request/message tests prove observable OpenAI-compatible equivalence;
- provider and runtime focused tests pass.

### W1-B Runtime RunStateStore

Primary ownership:

- `modules/agent/internal/runtime/runtime.go`
- `goal_loop.go`
- `todo_run_state.go`
- event sequence and run lifecycle helpers
- new focused store tests

Target model:

```go
type RunState struct {
    Cancel    context.CancelFunc
    RunSeq    uint64
    WorkerSeq map[string]uint64
    Todos     []methods.TodoItemDTO
    Goal      *runGoalState
}
```

Implementation:

1. Add one concurrency-safe store for Runtime-owned per-run state.
2. Route run registration, cancellation lookup, sequences, Todo, and Goal snapshots through it.
3. Make run removal clear all Runtime-owned snapshots in one operation.
4. Keep WorkerPool and MCP Manager as separate resource owners.
5. Keep copy-on-read/copy-on-write behavior for mutable slices and Goal state.

Acceptance:

- Runtime no longer maintains separate `activeRuns`, `nextSeq`, `agentSeq`, `runTodos`, and `runGoals` maps;
- terminal/unregister cleanup removes all store state and MCP bindings;
- sequence and concurrent access tests pass, including race-focused tests;
- existing Runtime behavior remains unchanged.

### W1-C Desktop Settings resource objects

Primary ownership:

- `useGatewayResources.js`
- `App.jsx`
- `SettingsPanel.jsx`
- Settings tab components only when required
- focused frontend tests

Target shape:

```js
{
  providers: { items, loading, error, load, create, update, remove },
  workers:   { items, loading, error, load, create, update, remove },
  mcp:       { items, loading, error, discoveryById, load, create, update, remove, discover },
  skills:    { items, loading, error, load, loadDetail, create, update, remove }
}
```

Implementation:

1. Return stable domain resource objects from `useGatewayResources`.
2. Pass those objects through App and SettingsPanel instead of flat resource props.
3. Preserve local editor/draft state inside SettingsPanel.
4. Do not create a generic CRUD hook unless it removes real duplication without hiding resource-specific refresh semantics.

Acceptance:

- App and SettingsPanel resource prop surfaces are materially smaller;
- resource CRUD behavior and selected provider side effects remain unchanged;
- Settings tabs receive explicit domain state/actions;
- frontend unit tests and production build pass.

## 4. Parallel Ownership

| Worker | Owns | Must not edit |
| --- | --- | --- |
| Provider/Prompt | W1-A provider files, `runtime/loop.go`, new composer/tests | Runtime state files, frontend, Gateway |
| RunStateStore | W1-B Runtime state/lifecycle files and tests | provider files, `runtime/loop.go`, frontend, Gateway |
| Settings resources | W1-C frontend Settings resource flow and tests | Go code, Gateway, provider |
| Root integrator | plan, review, conflict resolution, full verification | unrelated behavior |

The Runtime workers intentionally own disjoint production files. Shared package tests may observe both changes, but workers must not overwrite files outside their scope.

## 5. Deferred Work

The next wave may address:

- Gateway `StateToolDispatcher` with compatibility adapters for four existing RPC methods;
- `RunContextAssembler` for Gateway provider/memory/todo/goal/worker/MCP preparation;
- `EventEmitter` extraction after RunStateStore stabilizes sequence ownership;
- canonical internal execution options with explicit wire DTO conversion;
- CSS feature modules and design tokens.

Explicitly deferred or rejected:

- generic GORM repository;
- merging Memory, Todo, Goal, and Context storage;
- universal ToolHandler reflection registry;
- one Runtime service locator containing every dependency.

## 6. Verification Gates

Focused:

```text
cd modules/agent
go test -count=1 ./internal/provider/...
go test -count=1 ./internal/runtime/...
go test -race ./internal/runtime/...

cd modules/desktop/frontend
npm test
npm run build
```

Integration:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/ci-gate.ps1
```

Audit:

- `git diff --check` passes;
- Provider golden tests prove request equivalence;
- Runtime production code has one per-run state owner;
- Settings resource props are grouped by domain;
- only planned files changed.

## 7. Completion Criteria

Wave 1 is complete only after all three parallel workstreams are integrated, reviewed against their acceptance criteria, and the repository integration gate passes. Deferred work remains documented rather than being silently mixed into this batch.

## 8. Progress Log

### 2026-07-15 - Wave 1 implementation

| Workstream | Result | Verification |
| --- | --- | --- |
| W1-A Provider/Prompt | Provider owns neutral request/message/options types; Runtime `promptComposer` owns prompt policy and ordering; provider production code no longer imports `protocol/methods` | golden serialization and prompt-order tests, focused/full Agent tests, Runtime race, `go vet` |
| W1-B RunStateStore | One concurrency-safe store owns registration, cancellation lookup, sequences, Todo/Goal snapshots, and removal; old five maps are removed | store copy/lifecycle/concurrency tests, focused/full Agent tests, Runtime race |
| W1-C Settings resources | Gateway resources are stable domain objects; App passes four resource props to SettingsPanel instead of the prior flat surface | 142 frontend tests and production build |

Settings Playwright navigation/layout/MCP discovery checks passed. Two skill-management checks require a running Gateway and workspace; in the fixture-only environment the create action remains correctly disabled and requests report `127.0.0.1:17931` unavailable. They are not used as evidence for this structural batch.

Gateway StateToolDispatcher, RunContextAssembler, EventEmitter, canonical execution options, and CSS modules remain deferred exactly as listed in section 5.
