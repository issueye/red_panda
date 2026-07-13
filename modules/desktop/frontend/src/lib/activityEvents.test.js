import assert from 'node:assert/strict';
import test from 'node:test';
import {
  classifyRunEventKind,
  filterRunEvents,
  formatRunEventPayload,
  getRunEventFilterOptions,
  getRunEventTimelineMeta,
  groupRunEventsByKind,
  normalizeRunEvent,
  summarizeRunEvent,
} from './activityEvents.js';

function event(overrides = {}) {
  return normalizeRunEvent({
    protocol_version: '2026-07-13',
    event_id: 'evt_1',
    type: 'message_delta',
    run_id: 'run_1',
    session_id: 'session_1',
    assignment_id: 'assignment_1',
    run_seq: 4,
    worker_seq: 2,
    worker: { id: 'worker-01', profile_key: 'reviewer' },
    payload: { delta: 'review complete' },
    created_at: '2026-07-13T00:00:00Z',
    ...overrides,
  });
}

test('normalizeRunEvent maps only EnvelopeV2 Worker fields', () => {
  const value = event({ root_run_id: 'legacy', root_seq: 99, agent_role: 'subagent' });
  assert.equal(value.protocolVersion, '2026-07-13');
  assert.equal(value.runId, 'run_1');
  assert.equal(value.assignmentId, 'assignment_1');
  assert.equal(value.runSeq, 4);
  assert.equal(value.workerSeq, 2);
  assert.equal(value.workerId, 'worker-01');
  assert.equal(value.profileKey, 'reviewer');
  assert.equal(value.rootRunId, undefined);
  assert.equal(value.agentRole, undefined);
});

test('timeline metadata uses Worker scope and v0.2 sequences', () => {
  const meta = getRunEventTimelineMeta(event());
  assert.equal(meta.scope, 'Worker · worker-01 · reviewer');
  assert.equal(meta.sequence, '运行事件 004 · Worker 事件 002');
  assert.equal(meta.summary, 'review complete');
});

test('summaries classify tool, permission, error and terminal events', () => {
  assert.match(summarizeRunEvent(event({ type: 'tool_started', payload: {
    tool_name: 'workspace.read_file', status: 'running', arguments: { path: 'README.md' },
  } })), /workspace\.read_file/);
  assert.match(summarizeRunEvent(event({ type: 'permission_required', payload: {
    tool_name: 'shell.exec', summary: 'Allow command', risk: 'high',
  } })), /授权/);
  assert.equal(classifyRunEventKind('worker_assignment_updated', { status: 'failed' }), 'event');
  assert.equal(classifyRunEventKind('error', { error: 'boom' }), 'error');
  assert.equal(classifyRunEventKind('finish', { status: 'completed' }), 'done');
});

test('filtering, grouping and options use stable run_seq ordering', () => {
  const items = [
    event({ event_id: 'evt_3', run_seq: 3, type: 'finish', payload: { status: 'completed' } }),
    event({ event_id: 'evt_1', run_seq: 1, type: 'tool_started', payload: { tool_name: 'workspace.list' } }),
    event({ event_id: 'evt_2', run_seq: 2, worker: { id: 'worker-02' }, type: 'error', payload: { error: 'failed' } }),
  ];
  assert.deepEqual(filterRunEvents(items).map((item) => item.id), ['evt_1', 'evt_2', 'evt_3']);
  assert.deepEqual(filterRunEvents(items, { kind: 'error' }).map((item) => item.id), ['evt_2']);
  assert.deepEqual(groupRunEventsByKind(items).map((item) => item.kind), ['error', 'tool', 'done']);
  const options = getRunEventFilterOptions(items);
  assert.deepEqual(options.kinds, ['all', 'error', 'tool', 'done']);
  assert.ok(options.scopes.includes('Worker · worker-02'));
});

test('formatRunEventPayload pretty prints and truncates payloads', () => {
  assert.equal(formatRunEventPayload({ payload: {} }), '');
  assert.match(formatRunEventPayload({ payload: { status: 'completed' } }), /\n/);
  assert.match(formatRunEventPayload({ payload: { value: 'x'.repeat(4100) } }), /已截断$/);
});
