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

test('normalizeRunEvent maps gateway event dto fields', () => {
  const event = normalizeRunEvent({
    id: 'evt_1',
    type: 'message_delta',
    root_run_id: 'run_1',
    run_id: 'run_1:subagent:planner',
    root_seq: 4,
    agent_role: 'subagent',
    agent_name: 'planner',
    stream_kind: 'message',
    payload: { delta: 'planner ready' },
  });

  assert.equal(event.id, 'evt_1');
  assert.equal(event.rootRunId, 'run_1');
  assert.equal(event.agentRole, 'subagent');
  assert.equal(event.agentName, 'planner');
  assert.equal(event.streamKind, 'message');
  assert.equal(event.rootSeq, 4);
  assert.equal(event.eventKind, 'message');
});

test('summarizeRunEvent prefers payload delta and trims long text', () => {
  const summary = summarizeRunEvent({
    payload: { delta: `hello ${'x'.repeat(120)}` },
  });
  assert.equal(summary.length, 96);
  assert.equal(summary.endsWith('...'), true);
});

test('getRunEventTimelineMeta distinguishes subagent scope and sequence', () => {
  const event = normalizeRunEvent({
    id: 'evt_2',
    type: 'tool_started',
    root_seq: 8,
    agent_seq: 3,
    agent_role: 'subagent',
    agent_name: 'researcher',
    payload: { tool_name: 'web_search', status: 'running', input: { q: 'issue timeline' } },
  });

  const meta = getRunEventTimelineMeta(event);

  assert.equal(meta.kind, 'tool');
  assert.equal(meta.scope, '子代理 · researcher');
  assert.equal(meta.sequence, '事件 008 · 代理事件 003');
  assert.equal(meta.title, 'tool_started');
  assert.equal(meta.summary, 'web_search - 运行中 - q=issue timeline');
});

test('getRunEventTimelineMeta keeps root events compact', () => {
  const meta = getRunEventTimelineMeta(normalizeRunEvent({
    id: 'evt_3',
    type: 'run_completed',
    root_seq: 9,
    agent_role: 'root',
    payload: { status: 'completed' },
  }));

  assert.equal(meta.kind, 'done');
  assert.equal(meta.scope, '主代理');
  assert.equal(meta.sequence, '事件 009');
  assert.equal(meta.summary, '完成 - 已完成');
});

test('summarizeRunEvent formats permission and error payloads', () => {
  assert.equal(summarizeRunEvent({
    type: 'permission_request',
    payload: {
      action: 'pending',
      tool_name: 'shell_command',
      reason: 'writes files',
    },
  }), '授权 - 待处理 - shell_command - writes files');

  assert.equal(summarizeRunEvent({
    type: 'run_error',
    payload: { error: 'permission denied' },
  }), 'permission denied');
});

test('classifyRunEventKind recognizes common event families', () => {
  assert.equal(classifyRunEventKind('tool_call_delta', '', {}), 'tool');
  assert.equal(classifyRunEventKind('permission_resolved', '', {}), 'permission');
  assert.equal(classifyRunEventKind('memory_injected', '', { memory_ids: ['mem_1'] }), 'memory');
  assert.equal(classifyRunEventKind('message_delta', '', { delta: 'hi' }), 'message');
  assert.equal(classifyRunEventKind('run_finished', '', {}), 'done');
  assert.equal(classifyRunEventKind('unknown', '', {}), 'event');
});

test('filterRunEvents applies kind and agent scope filters', () => {
  const events = [
    normalizeRunEvent({
      id: 'evt_root_tool',
      type: 'tool_started',
      root_seq: 1,
      agent_role: 'root',
      payload: { tool_name: 'workspace.read_file' },
    }),
    normalizeRunEvent({
      id: 'evt_sub_message',
      type: 'message_delta',
      root_seq: 2,
      agent_seq: 1,
      agent_role: 'subagent',
      agent_name: 'planner',
      payload: { delta: 'plan' },
    }),
    normalizeRunEvent({
      id: 'evt_sub_tool',
      type: 'tool_finished',
      root_seq: 3,
      agent_seq: 2,
      agent_role: 'subagent',
      agent_name: 'planner',
      payload: { tool_name: 'workspace.grep' },
    }),
  ];

  assert.deepEqual(
    filterRunEvents(events, { kind: 'tool', scope: '子代理 · planner' }).map((event) => event.id),
    ['evt_sub_tool'],
  );
  assert.deepEqual(
    filterRunEvents(events, { kind: 'all', scope: '主代理' }).map((event) => event.id),
    ['evt_root_tool'],
  );
  assert.deepEqual(filterRunEvents(null, { kind: 'tool', scope: '主代理' }), []);
});

test('groupRunEventsByKind returns stable inspection groups with ordered events', () => {
  const groups = groupRunEventsByKind([
    normalizeRunEvent({ id: 'evt_message', type: 'message_delta', root_seq: 4, payload: { delta: 'hi' } }),
    normalizeRunEvent({ id: 'evt_tool_done', type: 'tool_finished', root_seq: 3, payload: { tool_name: 'shell.exec' } }),
    normalizeRunEvent({ id: 'evt_error', type: 'error', root_seq: 1, payload: { error: 'failed' } }),
    normalizeRunEvent({ id: 'evt_tool', type: 'tool_started', root_seq: 2, payload: { tool_name: 'shell.exec' } }),
  ]);

  assert.deepEqual(groups.map((group) => `${group.kind}:${group.count}`), [
    'error:1',
    'tool:2',
    'message:1',
  ]);
  assert.deepEqual(groups.find((group) => group.kind === 'tool').events.map((event) => event.id), [
    'evt_tool',
    'evt_tool_done',
  ]);
});

test('getRunEventFilterOptions includes all option and sorted scopes', () => {
  const options = getRunEventFilterOptions([
    normalizeRunEvent({ id: 'evt_root', type: 'finish', agent_role: 'root' }),
    normalizeRunEvent({ id: 'evt_sub', type: 'permission_required', agent_role: 'subagent', agent_name: 'coder' }),
    normalizeRunEvent({ id: 'evt_runtime', type: 'message_delta', agent_role: 'runtime', agent_name: 'worker' }),
  ]);

  assert.deepEqual(options.kinds, ['all', 'permission', 'message', 'done']);
  assert.deepEqual(options.scopes, ['all', 'runtime · worker', '主代理', '子代理 · coder']);
});

test('formatRunEventPayload returns pretty JSON only when payload exists', () => {
  assert.equal(formatRunEventPayload({ payload: {} }), '');
  assert.equal(
    formatRunEventPayload({ payload: { tool_name: 'workspace.list', arguments: { path: '.' } } }),
    '{\n  "tool_name": "workspace.list",\n  "arguments": {\n    "path": "."\n  }\n}',
  );
  const longPayload = formatRunEventPayload({ payload: { output: 'x'.repeat(5000) } });
  assert.equal(longPayload.endsWith('... 已截断'), true);
  assert.equal(longPayload.length < 4100, true);
});

test('getRunEventTimelineMeta preserves non-subagent roles', () => {
  const meta = getRunEventTimelineMeta(normalizeRunEvent({
    id: 'evt_runtime',
    type: 'message_delta',
    agent_role: 'runtime',
    agent_name: 'pool',
    payload: { message: 'ready' },
  }));

  assert.equal(meta.scope, 'runtime · pool');
});
