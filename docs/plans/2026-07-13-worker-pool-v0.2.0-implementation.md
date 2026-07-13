# Worker Pool v0.2.0 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use executing-plans to implement this plan task-by-task.

**Goal:** Replace the root/subagent execution model with an eagerly constructed peer WorkerPool whose delegated assignments cannot delegate again.

**Architecture:** Runtime owns one WorkerPool. The pool owns stable Worker slots, mailboxes, reusable Executors, and the Assignment ledger. Gateway-created entry assignments may delegate one level; assignments with an origin worker cannot submit another assignment. Protocol, Gateway, Runtime, and Desktop switch atomically to Worker/Assignment terminology without v0.1 aliases.

**Tech Stack:** Go 1.24 workspace, JSON-RPC, Gin/GORM/SQLite, React/Vite, Node test runner, Playwright.

---

### Task 1: Implement the Worker core package

**Files:**
- Create: `modules/agent/internal/worker/types.go`
- Create: `modules/agent/internal/worker/mailbox.go`
- Create: `modules/agent/internal/worker/pool.go`
- Create: `modules/agent/internal/worker/pool_test.go`

**Steps:**
1. Write tests for eager Worker creation, stable IDs, assignment terminal states, bounded mailbox delivery, cancellation, and pool close.
2. Write tests proving delegated assignments return `ErrNestedDelegation` even when origin fields are spoofed in task payloads.
3. Implement Worker, Assignment, Executor, Mailbox, MessageBus, Pool, snapshots, capacity errors, and single-level delegation validation.
4. Run `go test ./modules/agent/internal/worker -count=1` and expect PASS.

### Task 2: Define the v0.2 protocol contract

**Files:**
- Modify: `modules/protocol/events/events.go`
- Modify: `modules/protocol/methods/methods.go`
- Create: `modules/protocol/events/worker_events_test.go`
- Create: `modules/protocol/methods/worker_methods_test.go`

**Steps:**
1. Add WorkerRef, Assignment record/status, Worker list/cancel/message/pool request and result types.
2. Add `run.execute`, `run.cancel`, `worker.list`, `worker.assignment.cancel`, `worker.message.send`, and `worker.pool.status` constants.
3. Define the v0.2 event envelope shape using run/assignment/worker identifiers without parent/child fields.
4. Keep the old contract compiling only until Task 5; do not add adapters or aliases from old names to new names.
5. Run `go test ./modules/protocol/... -count=1` and expect PASS.

### Task 3: Add WorkerProfile persistence and migration

**Files:**
- Modify: `modules/gateway/internal/gateway/model/models.go`
- Modify: `modules/gateway/internal/gateway/infra/database/migrate.go`
- Create: `modules/gateway/internal/gateway/infra/database/worker_profile_migrate_test.go`

**Steps:**
1. Add WorkerProfile with the v0.2 configuration fields and `worker_*` identifiers.
2. Add an idempotent migration that copies existing AgentDefinition rows into WorkerProfile rows without deleting source data in this first batch.
3. Test empty database migration, data copy, repeated migration, and preservation of customized prompts/tool policy.
4. Run `go test ./modules/gateway/internal/gateway/infra/database -count=1` and expect PASS.

### Task 4: Wire Runtime entry assignments and Worker tools

**Files:**
- Modify: `modules/agent/internal/runtime/runtime.go`
- Create: `modules/agent/internal/runtime/worker_runtime.go`
- Create: `modules/agent/internal/runtime/worker_tools.go`
- Modify: `modules/agent/internal/tools/runner.go`
- Modify: `modules/agent/internal/runtime/tool_batch.go`
- Add/modify focused Runtime tests.

**Steps:**
1. Construct and start WorkerPool in Runtime initialization.
2. Route every external run through an entry Assignment.
3. Replace model-facing `subagent.*` tools with `worker.*` tools.
4. Build tools per Assignment: entry includes `worker.delegate`; delegated assignments do not.
5. Pass caller assignment identity out-of-band to Pool and reject nested delegation there.
6. Preserve `worker.send` and `worker.receive` for delegated assignments so the delegation restriction does not disable peer communication.
7. Test that rejected nested delegation creates no Assignment, emits no assignment-start event, and leaves Pool capacity unchanged.
8. Route planner, Goal specialists, and isolated skills through Pool Submit.
9. Run agent Runtime and tool tests and expect PASS.

### Task 5: Switch Gateway and Desktop consumers

**Files:**
- Modify Gateway RuntimeClient, services, controllers, repositories, routes, and tests.
- Rename AgentDefinition service/API to WorkerProfile.
- Modify Desktop API helpers, reducers, settings, activity panel, fixtures, and tests.

**Steps:**
1. Switch Gateway RuntimeClient to Run/Worker methods and v0.2 events.
2. Remove Gateway-side subagent enumeration during Run cancellation.
3. Expose WorkerProfile CRUD and migrate tool audit fields.
4. Replace SubAgentPanel/status reducer/settings with Worker/Assignment UI.
5. Remove `/subagent`, backend selection, and spawn-subagent options.
6. Run Gateway tests, frontend unit tests, and Playwright coverage.

### Task 6: Delete the old model and release 0.2.0

**Files:**
- Delete: `modules/agent/internal/subagent/**`
- Delete old SubAgent Runtime adapters/tests and frontend modules.
- Modify version/build/release documentation files.

**Steps:**
1. Delete old Registry, Coordinator, ProcessPool, RPCs, tools, event fields, UI, and tests.
2. Search production source for `SubAgent`, `subagent`, root-agent roles, and parent/child run identifiers; remove remaining domain usages.
3. Set product build version to `0.2.0` across Gateway, Agent, Desktop, and CLI release inputs.
4. Run `go test ./modules/...`, frontend unit tests, Gateway-backed E2E, CI gate, and process leak checks.
5. Update architecture/status/release documentation with actual verification evidence.
