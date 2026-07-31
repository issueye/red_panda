import assert from 'node:assert/strict';
import test from 'node:test';
import { createEmptySessionRuntime } from './sessionRuntime.js';
import {
  appendWorkerText,
  reconcileAssignmentsWithRuns,
  reduceRunEvent,
  WORKER_PROTOCOL_VERSION,
} from './reduceRunEvent.js';

const event = (type, runSeq, payload = {}) => ({
  protocol_version: WORKER_PROTOCOL_VERSION,
  event_id: `evt_${runSeq}`,
  run_id: 'run_1',
  session_id: 'session_1',
  assignment_id: 'assignment_1',
  run_seq: runSeq,
  worker_seq: runSeq,
  worker: { id: 'worker-01', profile_key: 'general' },
  type,
  payload,
});

const eventForRun = (runId, type, runSeq, payload = {}) => ({
  ...event(type, runSeq, payload),
  event_id: `evt_${runId}_${runSeq}`,
  run_id: runId,
  assignment_id: `assignment_${runId}`,
});

test('rejects legacy live event envelopes', () => {
  const base = createEmptySessionRuntime();
  const { runtime, effects } = reduceRunEvent(base, { root_run_id: 'run_1', root_seq: 1, type: 'finish' });
  assert.equal(runtime, base);
  assert.equal(effects[0].type, 'ignore_incompatible_event');
});

test('rejects incomplete v0.2 envelopes', () => {
  const base = createEmptySessionRuntime();
  const incomplete = event('message_delta', 1, { delta: 'ignored' });
  delete incomplete.worker;
  const { runtime, effects } = reduceRunEvent(base, incomplete);
  assert.equal(runtime, base);
  assert.equal(effects[0].type, 'ignore_incompatible_event');
});

test('projects tool and permission with Worker identity', () => {
  const base = createEmptySessionRuntime({ runs: [{ id: 'run_1', status: 'running' }] });
  const tool = reduceRunEvent(base, event('tool_started', 1, {
    tool_call_id: 'tool_1', tool_name: 'workspace.read_file', arguments: { path: 'README.md' },
  })).runtime;
  assert.equal(tool.tools[0].workerId, 'worker-01');
  assert.equal(tool.tools[0].assignmentId, 'assignment_1');
  const permission = reduceRunEvent(tool, event('permission_required', 2, {
    permission_id: 'permission_1', summary: 'Allow read', tool_name: 'workspace.read_file',
  }));
  assert.equal(permission.runtime.permissions[0].assignmentId, 'assignment_1');
  assert.ok(permission.effects.some((item) => item.type === 'upsert_global_permission'));
});

test('assignment terminal state is irreversible', () => {
  const base = createEmptySessionRuntime();
  const completed = reduceRunEvent(base, event('worker_assignment_updated', 1, { status: 'completed' })).runtime;
  const late = reduceRunEvent(completed, event('worker_assignment_updated', 2, { status: 'running' })).runtime;
  assert.equal(late.assignmentsById.assignment_1.status, 'completed');
  assert.deepEqual(late.assignmentOrder, ['assignment_1']);
});

test('paused assignment remains active and can return to running', () => {
  const base = createEmptySessionRuntime({
    running: true,
    currentRunId: 'run_1',
    runs: [{ id: 'run_1', status: 'running' }],
  });
  const paused = reduceRunEvent(base, event('worker_assignment_updated', 1, { status: 'paused' })).runtime;
  assert.equal(paused.assignmentsById.assignment_1.status, 'paused');
  assert.equal(paused.running, true);
  assert.equal(paused.currentRunId, 'run_1');

  const resumed = reduceRunEvent(paused, event('worker_assignment_updated', 2, { status: 'running' })).runtime;
  assert.equal(resumed.assignmentsById.assignment_1.status, 'running');
  assert.equal(resumed.currentRunId, 'run_1');
});

test('worker private text never enters main conversation', () => {
  const base = createEmptySessionRuntime();
  const next = reduceRunEvent(base, event('message_delta', 1, {
    delta: 'private report', visibility: 'worker_private',
  })).runtime;
  assert.equal(next.messages.length, 0);
});

test('finish closes run and pending permission', () => {
  const base = createEmptySessionRuntime({
    running: true, currentRunId: 'run_1',
    runs: [{ id: 'run_1', status: 'running' }],
    permissions: [{ id: 'permission_1', runId: 'run_1', status: 'pending' }],
    assignmentsById: {
      assignment_1: { id: 'assignment_1', runId: 'run_1', status: 'running' },
    },
    assignmentOrder: ['assignment_1'],
  });
  const { runtime, effects } = reduceRunEvent(base, event('finish', 3, { status: 'completed' }));
  assert.equal(runtime.running, false);
  assert.equal(runtime.runs[0].status, 'completed');
  assert.equal(runtime.permissions[0].status, 'closed');
  assert.equal(runtime.assignmentsById.assignment_1.status, 'completed');
  assert.deepEqual(effects, [{ type: 'clear_global_permissions_for_run', runId: 'run_1' }]);
});

test('worker list snapshots cannot reopen assignments from terminal runs', () => {
  const reconciled = reconcileAssignmentsWithRuns([
    { id: 'assignment_1', runId: 'run_1', status: 'running' },
    { id: 'assignment_2', runId: 'run_2', status: 'running' },
  ], [
    { id: 'run_1', status: 'completed', finishedAt: '2026-07-14T05:08:42Z' },
    { id: 'run_2', status: 'running' },
  ]);
  assert.equal(reconciled[0].status, 'completed');
  assert.equal(reconciled[0].finishedAt, '2026-07-14T05:08:42Z');
  assert.equal(reconciled[1].status, 'running');
});

test('terminal Run state is irreversible', () => {
  const base = createEmptySessionRuntime({
    running: true,
    currentRunId: 'run_1',
  });
  const finished = reduceRunEvent(base, event('finish', 1, { status: 'completed' })).runtime;
  const late = reduceRunEvent(finished, event('message_delta', 2, { delta: 'late' })).runtime;
  assert.equal(late, finished);
  assert.equal(late.running, false);
  assert.equal(late.messages.length, 0);
});

test('new Run events are not suppressed by a completed Goal Run high sequence', () => {
  const base = createEmptySessionRuntime({
    running: true,
    currentRunId: 'run_followup',
    runSeq: 172,
    runSeqByRun: { run_goal: 172 },
    runs: [
      { id: 'run_goal', status: 'completed', lastRunSeq: 172 },
      { id: 'run_followup', status: 'running', lastRunSeq: 0 },
    ],
  });
  const replied = reduceRunEvent(base, eventForRun('run_followup', 'message_delta', 1, {
    delta: 'follow-up answer',
  })).runtime;
  assert.equal(replied.messages.at(-1).text, 'follow-up answer');
  assert.equal(replied.runSeq, 1);
  assert.equal(replied.runSeqByRun.run_goal, 172);
  assert.equal(replied.runSeqByRun.run_followup, 1);

  const finished = reduceRunEvent(replied, eventForRun('run_followup', 'finish', 2, {
    status: 'completed',
  })).runtime;
  assert.equal(finished.running, false);
  assert.equal(finished.currentRunId, '');
});

test('appendWorkerText merges consecutive deltas for one assignment', () => {
  const first = appendWorkerText([], event('message_delta', 1, { delta: 'Hello' }), 'Hello');
  const newline = appendWorkerText(first, event('message_delta', 2, { delta: '\n\n' }), '\n\n');
  const second = appendWorkerText(newline, event('message_delta', 3, { delta: '## Report' }), '## Report');
  assert.equal(second.length, 1);
  assert.equal(second[0].text, 'Hello\n\n## Report');
});

test('reasoning deltas stay separate from the final assistant answer', () => {
  const base = createEmptySessionRuntime();
  const first = reduceRunEvent(base, event('reasoning_delta', 1, { delta: 'Inspect files. ' })).runtime;
  const second = reduceRunEvent(first, event('reasoning_delta', 2, { delta: 'Check tests.' })).runtime;
  const answer = reduceRunEvent(second, event('message_delta', 3, { delta: 'Done.' })).runtime;

  assert.equal(answer.messages.length, 2);
  assert.equal(answer.messages[0].role, 'reasoning');
  assert.equal(answer.messages[0].text, 'Inspect files. Check tests.');
  assert.equal(answer.messages[1].role, 'assistant');
  assert.equal(answer.messages[1].text, 'Done.');
});
