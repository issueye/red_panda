import assert from 'node:assert/strict';
import test from 'node:test';

import { createEmptySessionRuntime } from './sessionRuntime.js';
import { appendAgentText, reduceRunEvent } from './reduceRunEvent.js';

test('reduceRunEvent tool_started projects tool card and run counters', () => {
  const base = createEmptySessionRuntime({
    runs: [{ id: 'run_1', status: 'running', toolCount: 0 }],
  });
  const { runtime, effects } = reduceRunEvent(base, {
    type: 'tool_started',
    root_run_id: 'run_1',
    root_seq: 3,
    payload: {
      tool_call_id: 't1',
      tool_name: 'workspace.read_file',
      display_name: 'Read',
      risk: 'low',
      arguments: { path: 'README.md' },
    },
  });
  assert.equal(effects.length, 0);
  assert.equal(runtime.tools.length, 1);
  assert.equal(runtime.tools[0].name, 'workspace.read_file');
  assert.equal(runtime.tools[0].status, 'running');
  assert.equal(runtime.runs[0].toolCount, 1);
  assert.equal(runtime.rootSeq, 3);
});

test('reduceRunEvent permission_required emits global permission effects', () => {
  const base = createEmptySessionRuntime({
    runs: [{ id: 'run_1', status: 'running' }],
  });
  const { runtime, effects } = reduceRunEvent(base, {
    type: 'permission_required',
    root_run_id: 'run_1',
    session_id: 'sess_1',
    root_seq: 4,
    payload: {
      permission_id: 'perm_1',
      run_id: 'run_1',
      summary: '运行 shell',
      tool_name: 'shell.exec',
      risk: 'high',
    },
  });
  assert.equal(runtime.permissions.length, 1);
  assert.equal(runtime.permissions[0].id, 'perm_1');
  assert.equal(runtime.runs[0].status, 'waiting_permission');
  assert.ok(effects.some((e) => e.type === 'upsert_global_permission'));
  assert.ok(effects.some((e) => e.type === 'select_activity_tab'));
});

test('reduceRunEvent root finish clears run and pending permissions effect', () => {
  const base = createEmptySessionRuntime({
    running: true,
    currentRunId: 'run_1',
    permissions: [{ id: 'p1', runId: 'run_1', status: 'pending' }],
    runs: [{ id: 'run_1', status: 'running' }],
    runEventsByRun: { run_1: [{ id: 'e1' }] },
  });
  const { runtime, effects } = reduceRunEvent(base, {
    type: 'finish',
    root_run_id: 'run_1',
    root_seq: 9,
    agent: { role: 'root', name: 'root' },
    payload: { status: 'completed' },
  });
  assert.equal(runtime.running, false);
  assert.equal(runtime.currentRunId, '');
  assert.equal(runtime.runs[0].status, 'completed');
  assert.equal(runtime.permissions[0].status, 'closed');
  assert.equal(runtime.runEventsByRun.run_1, undefined);
  assert.deepEqual(effects, [{ type: 'clear_global_permissions_for_run', runId: 'run_1' }]);
});

test('reduceRunEvent todo_updated auto-expands once when open count rises', () => {
  const base = createEmptySessionRuntime({ todoOpenCount: 0 });
  const { runtime } = reduceRunEvent(base, {
    type: 'todo_updated',
    root_run_id: 'run_1',
    root_seq: 2,
    payload: {
      items: [{ id: '1', content: 'step', status: 'pending' }],
      open_count: 1,
    },
  });
  assert.equal(runtime.todoOpenCount, 1);
  assert.equal(runtime.todosExpanded, true);
  assert.equal(runtime.todosAutoExpandedOnce, true);
});

test('reduceRunEvent goal_updated focuses active goal', () => {
  const base = createEmptySessionRuntime();
  const { runtime } = reduceRunEvent(base, {
    type: 'goal_updated',
    root_run_id: 'run_1',
    root_seq: 5,
    payload: {
      goal: {
        id: 'g1',
        status: 'active',
        objective: 'ship feature',
        pipeline_phase: 'execute',
      },
    },
  });
  assert.equal(runtime.goal?.id, 'g1');
  assert.equal(runtime.goalExpanded, true);
  assert.equal(runtime.goals.length, 1);
});

test('appendAgentText merges consecutive message_delta on same stream', () => {
  const first = appendAgentText([], {
    type: 'message_delta',
    root_run_id: 'run_1',
    run_id: 'run_1',
    root_seq: 1,
    event_id: 'e1',
    agent: { role: 'root', name: 'root' },
    payload: { delta: 'Hello' },
  }, 'Hello');
  const second = appendAgentText(first, {
    type: 'message_delta',
    root_run_id: 'run_1',
    run_id: 'run_1',
    root_seq: 2,
    agent: { role: 'root', name: 'root' },
    payload: { delta: ' world' },
  }, ' world');
  assert.equal(second.length, 1);
  assert.equal(second[0].text, 'Hello world');
});

test('subagent finish does not terminate root run via reduceRunEvent path', () => {
  const base = createEmptySessionRuntime({
    running: true,
    currentRunId: 'run_1',
    runs: [{ id: 'run_1', status: 'running' }],
  });
  const { runtime, effects } = reduceRunEvent(base, {
    type: 'finish',
    root_run_id: 'run_1',
    root_seq: 8,
    agent: { role: 'subagent', name: 'worker', subagent_id: 's1' },
    payload: { status: 'completed' },
  });
  // Subagent finish is not a root terminal event — root run stays active.
  assert.equal(runtime.running, true);
  assert.equal(runtime.currentRunId, 'run_1');
  assert.equal(effects.length, 0);
});
