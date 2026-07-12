# Goal Implementation Optimization Plan

## Purpose

Bring the current Goal prototype to a reliable controlled-execution implementation. The
Gateway remains the state authority, Runtime executes bounded segments, and Desktop is a
projection of persisted state.

## Invariants

1. A session has at most one active Goal, enforced by SQLite as well as service checks.
2. `goal.checkpoint` and `goal.complete` may mutate only the Goal bound to the calling run.
3. Terminal transitions are compare-and-swap operations. A late tool result cannot revive or
   overwrite a terminal Goal.
4. Cancelling an active Goal also cancels its active run.
5. Segment accounting is idempotent by `(goal_id, run_id, segment_index)`.
6. Goal limits can only tighten client run limits. A client option cannot raise a Goal budget.
7. Wall-time applies to the currently running Goal, including provider, tools, and subagents.
8. A root message stream has one terminal `final=true`; an intermediate segment cannot close it.
9. Every persisted lifecycle transition is observable by Desktop or followed by hydration.

## Delivery Phases

### Phase 1: State correctness

- Add a database constraint for one active Goal per session.
- Require active status and current-run binding for checkpoint/complete.
- Replace read-modify-save terminal transitions with conditional updates.
- Repair stale state before selecting a Goal for bind, then reload it.
- Roll back or pause a Goal if Runtime admission fails after binding.
- Make the cancel endpoint stop the active run before terminalizing the Goal.

Acceptance:

- An unbound run cannot checkpoint or complete a Goal.
- Pending, paused, succeeded, failed, and cancelled Goals cannot be completed by Runtime tools.
- Concurrent terminal mutations cannot overwrite an existing terminal state.
- A stale active Goal can be continued in one request.

### Phase 2: Budget correctness

- Persist a segment ledger with a unique `(goal_id, run_id, segment_index)` key.
- Apply segment counters and ledger insertion in one transaction.
- Make Goal segment limits authoritative and clamp client limits downward.
- Carry remaining wall-time to Runtime and apply it as a run deadline.
- Account bound-run wall-time once on root finish.
- Keep auto-continue disabled until its counter and permission gates are implemented.

Acceptance:

- Replaying `segment_end` does not change counters twice.
- A configured 12-turn Goal segment cannot be raised to 48 by run options.
- A timed-out provider/tool/subagent causes `failed/budget_exhausted` rather than an unbounded run.

### Phase 3: Stream and lifecycle correctness

- Suppress provider `final` on intermediate segments.
- Do not synthesize a final answer at a segment boundary that will continue.
- Emit one root terminal stream event and one root Finish event.
- Publish `goal_updated` for Gateway-owned pause/cancel/budget transitions, or explicitly hydrate
  Goals on every root terminal event.

Acceptance:

- No message delta is emitted after `final=true` for the same stream.
- Natural run completion changes the Goal strip from active to paused without reopening the session.

### Phase 4: Specialist and cost policy

- Keep Goal specialists stateless and root-owned.
- Keep `goal.*`, `todo.*`, and nested subagents denied for specialists.
- Include specialist elapsed time in the parent Goal wall budget.
- Add end-to-end tests for analyst -> planner -> implementer -> verifier -> evaluator flows.

## Verification Gates

- `go test ./...` in `modules/agent`, `modules/gateway`, and `modules/protocol`.
- `npm test -- --run` in `modules/desktop/frontend`.
- Focused tests for mutation authorization, stale recovery, cancellation, duplicate segments,
  budget clamping, stream final ordering, and terminal UI hydration.

