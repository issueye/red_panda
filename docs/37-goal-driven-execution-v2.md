# Goal-driven Execution V2

## Intent

Goal V2 is a feedback controller for completing an outcome. It is not a fixed
workflow and it is not a wrapper around the session TODO list.

The controller repeatedly answers four questions:

1. What observable result are we trying to produce?
2. What is the most useful next action given the current evidence?
3. What changed after that action?
4. Is the goal satisfied, still progressing, blocked, or no longer worth pursuing?

The model may research, plan, edit, test, delegate, or revise its approach in any
order. The persisted Goal state records the decision and evidence, not an imposed
analyze/plan/execute/verify phase.

## Domain model

### Goal contract

- `objective`: desired outcome in user language.
- `criteria[]`: independently assessable success conditions.
- `constraints[]`: boundaries that must remain true while pursuing the outcome.
- `strategy`: current high-level approach. It may be revised after an assessment.

### Goal actions

Actions belong to a Goal, not to the Session. They are a revisable queue rather
than a mandatory up-front plan.

Statuses: `queued | active | done | blocked | dropped`.

Each action carries its own acceptance condition, result, evidence, and attempt
count. At most one action is active for a Goal.

### Assessment

An assessment closes one control iteration. It contains:

- `verdict`: `progress | satisfied | blocked | no_progress`;
- an evidence-backed status for every criterion;
- the observed result and remaining gap;
- the next decision and optional next action.

`satisfied` is valid only when every criterion is `met`. A successful finish is
valid only after a persisted `satisfied` assessment.

### Decision journal

Lifecycle changes, plans, observations, and assessments are append-only events.
The Goal row is the current projection; the journal explains how it got there.

## Lifecycle

```text
active <-> paused
active -> succeeded | failed | cancelled
paused -> cancelled
```

There is no pipeline phase. A run is an execution lease on the active Goal.
Natural run completion pauses the lease with `awaiting_continue`; it does not
mean the outcome was reached.

## Runtime tools

- `goal.create`: create and optionally activate a Goal contract.
- `goal.plan`: replace or merge Goal actions and update the current strategy.
- `goal.observe`: record what an action produced, with evidence.
- `goal.assess`: compare the current state with every success criterion and choose
  the next control decision.
- `goal.finish`: terminalize after an assessment, with an outcome report.
- `goal.list`: inspect session Goals.

Goal tools are owned by the root run. Workers may execute or review actions but
cannot mutate the controller state.

## Budgets and stopping

- Runtime tool turns and wall time remain hard resource budgets.
- `max_iterations` bounds assessment cycles, not provider message segments.
- repeated `no_progress` assessments increment `stagnation_count`;
  `max_stagnation` pauses the Goal for user intervention.
- transport segments are an internal Runtime detail and are not shown as Goal
  progress.

## Product projection

Desktop shows the contract, active action, action queue, criterion status, last
assessment, and outcome. It does not show implementation phases or require the
user to understand the internal provider loop.
