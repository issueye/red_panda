import assert from 'node:assert/strict';
import test from 'node:test';
import { createEmptySessionRuntime } from './sessionRuntime.js';
import { appendWorkerText, reduceRunEvent, WORKER_PROTOCOL_VERSION } from './reduceRunEvent.js';

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
  });
  const { runtime, effects } = reduceRunEvent(base, event('finish', 3, { status: 'completed' }));
  assert.equal(runtime.running, false);
  assert.equal(runtime.runs[0].status, 'completed');
  assert.equal(runtime.permissions[0].status, 'closed');
  assert.deepEqual(effects, [{ type: 'clear_global_permissions_for_run', runId: 'run_1' }]);
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

test('appendWorkerText merges consecutive deltas for one assignment', () => {
  const first = appendWorkerText([], event('message_delta', 1, { delta: 'Hello' }), 'Hello');
  const second = appendWorkerText(first, event('message_delta', 2, { delta: ' world' }), ' world');
  assert.equal(second.length, 1);
  assert.equal(second[0].text, 'Hello world');
});
