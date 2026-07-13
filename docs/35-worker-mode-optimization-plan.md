# Worker Mode Optimization Plan

Updated: 2026-07-14

Status: Complete. All six phases and verification gates passed after a requirement-by-requirement audit on 2026-07-14.

## Objective

Make worker execution ordered, bounded, observable, recoverable, and consistent
with Gateway-owned Agent definitions. The optimization covers root-run admission,
Runtime subagent scheduling, process-pool lifecycle, cancellation, and verification.

## Invariants

1. Tool calls execute in provider order. Only adjacent `subagent.run` calls may run concurrently.
2. A worker is `queued` until it owns a process-pool slot; `running` means execution has started.
3. Root and worker concurrency share an authoritative Gateway resource budget.
4. A Runtime process exit terminalizes its root run and releases all persisted admission slots.
5. Pool acquisition is FIFO, context-cancellable, and does not poll.
6. Reused workers pass a health check; one cold-start retry is allowed after stale-worker failure.
7. Cancellation cannot depend on draining the worker event queue.
8. General workers have hard tool-turn and wall-time limits; child cost is observable by the root run.
9. Enabled Agent definitions are snapshotted at run start and enforced by Runtime.

## Delivery Phases

### Phase 1: Ordered worker execution

Status: Implemented and verified.

- Replace the global serial/parallel split in `executeToolBatch`.
- Execute ordinary tools in their original positions.
- Run only contiguous `subagent.run` groups concurrently.
- Preserve result ordering in provider tool history.
- Add mixed-batch, cancellation, and parallel-group regression tests.

Acceptance:

- `[worker, goal.update]` cannot update the Goal before the worker completes.
- `[worker A, worker B, ordinary tool, worker C]` runs A/B concurrently, then the ordinary tool, then C.
- Tool history remains in the exact provider call order.

### Phase 2: Fair queue and lifecycle visibility

Status: Implemented and verified.

- Replace 25 ms pool polling with a FIFO waiter queue or channel semaphore.
- Publish `queued`, `starting`, `running`, and terminal worker states.
- Add queue depth, wait duration, in-use, and idle metrics to pool status.
- Make release handles idempotent and safe during resize/reset.

Acceptance:

- Saturated workers start in FIFO order.
- Cancelling a queued worker removes it without consuming a slot.
- UI and Activity distinguish queue wait from execution time.

### Phase 3: Global resource governance

Status: Implemented and verified.

- Move the authoritative concurrency ceiling out of per-run client options.
- Add Gateway-wide root-process, worker-process, and provider-request budgets.
- Reserve/release worker capacity through Gateway or a shared local broker.
- Clamp Desktop preferences to server policy.

Acceptance:

- `per_run_process` cannot multiply worker capacity beyond the global limit.
- A client cannot raise the server concurrency ceiling.
- Status APIs expose configured limits and current usage.

### Phase 4: Crash recovery and worker health

Status: Implemented and verified.

- Emit a synthetic terminal event when a Runtime process exits unexpectedly.
- Reconcile persisted `running`/`waiting_permission` runs at Gateway startup.
- Add run leases or process ownership metadata for stale-run detection.
- Ping idle workers before reuse and retry once with a fresh process.

Acceptance:

- Killing a root Runtime releases its run slot and records a failed terminal state.
- Restarting Gateway does not leave stale runs blocking a session.
- A dead idle worker does not fail the user task on first reuse.

### Phase 5: Cancellation and backpressure

Status: Implemented and verified.

- Separate JSON-RPC responses from the bounded event-delivery queue.
- Ensure cancel/shutdown responses remain readable while event consumers are stalled.
- Bound graceful cancel and force-kill escalation times.

Acceptance:

- Cancelling with a saturated event queue terminates within the configured deadline.
- Critical lifecycle events remain persisted exactly once.

### Phase 6: Cost policy and Agent definitions

Status: Implemented and verified.

- Add hard general-worker turn, wall-time, output, and fan-out limits.
- Attribute child usage and elapsed time to the root run.
- Snapshot enabled Agent definitions into `ReplyOptions` at admission.
- Enforce custom prompts, allowlists, denylists, default turns, and disabled state in Runtime.

Acceptance:

- File count cannot create an unbounded worker budget.
- Disabled specialists are rejected or explicitly fall back to root execution.
- Custom Agent settings alter the corresponding worker behavior end to end.

## Verification Gates

1. Focused Runtime unit tests for each scheduler/pool invariant.
2. `go test -race ./internal/runtime` in `modules/agent`.
3. Gateway service, repository, and runtime-client tests including forced process exits.
4. Protocol compatibility coverage for queued/running/terminal worker states.
5. Gateway-backed Desktop tests for queue visibility, cancel, crash recovery, and Agent definitions.
6. Full module tests, frontend unit tests, Playwright tests, and production builds.

Verification result on 2026-07-14:

- Runtime scheduler and pool regression tests pass, including mixed ordered batches, FIFO acquisition, queued cancellation, idempotent release, dead-worker reuse, bounded output capture, and process-exit handling.
- All Go module tests pass for Protocol, Agent, Gateway, Desktop, and CLI.
- Race checks pass for Agent Runtime and Gateway service/runtime-client/repository packages.
- Frontend unit tests pass (97 tests), and the production frontend build passes.
- Gateway-backed Playwright passes 14/14, including queue visibility, Runtime crash recovery, and Agent-definition enforcement; the complete Playwright suite passes 29/29.
- `scripts/protocol-compat.ps1` passes the public HTTP/WebSocket compatibility matrix.
- `scripts/ws-smoke.ps1` passes, including observed `queued -> starting -> running -> completed` process-pool lifecycle.
- Agent and Gateway binaries were rebuilt, `git diff --check` passes, and verification leaves no Gateway/Agent test processes running.

## Completion Audit Evidence

| Requirement | Direct evidence |
| --- | --- |
| Ordered adjacent workers and ordinary-tool barriers | `worker_scheduler_test.go` covers parallel A/B, `goal.update` barrier, provider-order history, and cancellation before the following ordinary tool. |
| FIFO queue, cancellation, metrics, and lifecycle | Pool tests cover FIFO, queued cancellation, idempotent release, resize/reset and wait metrics; Gateway-backed UI covers visible queued -> starting -> running -> cancelled transitions. |
| Gateway-owned global budgets | `resource_budget_test.go` covers cross-root FIFO permits; RunService tests prove client limits are ignored and status exposes root, worker, and provider usage. |
| Crash recovery and ownership | Run records persist `runtime_owner`; startup reconciles only prior-owner active runs; synthetic root errors write `finished_at` and release worker/provider permits; Gateway-backed UI kills a real Runtime process and observes a failed terminal run. |
| Worker health | Pool tests replace an unhealthy idle process with one cold-start retry before user execution. |
| Backpressure and cancellation | Subagent process tests prove saturated event delivery does not block the RPC reader, optional events alone may drop, and critical events deliver once; cancel/shutdown deadlines remain 3s/2s before force-kill. Gateway inserts event IDs before projection/broadcast, so duplicate envelopes are ignored exactly once. |
| Hard cost limits | Runtime tests cover file-count/turn clamping, specialist definitions unable to exceed the Gateway cap, wall timeout, fan-out, and bounded 64 KiB output capture. Completion events expose queue/execution/elapsed time, tool counts, output bytes, and truncation. |
| Agent definitions | Gateway snapshots enabled and disabled records; Runtime tests enforce disabled state, prompt, allow/deny lists, and turns; Gateway-backed provider inspection proves the real child process receives the configured prompt, filtered tools, and turn budget. |
| External protocol | `ws-smoke.ps1` now asserts process-pool `queued -> starting -> running -> completed`; `protocol-compat.ps1` passes the complete public API/WebSocket matrix. |

## Execution Order

All phases were implemented in order and are complete. Worker ordering, admission
recovery, cancellation, resource budgets, and Agent definition enforcement are now
the stable baseline for later executable MCP integration.
