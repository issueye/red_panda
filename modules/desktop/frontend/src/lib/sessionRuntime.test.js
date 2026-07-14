import assert from 'node:assert/strict';
import test from 'node:test';

import {
  collectResumeCursors,
  collectSessionRunStatus,
  countActiveRuns,
  createEmptySessionRuntime,
  getSessionRuntime,
  patchSessionRuntimeMap,
  resolveEventSessionId,
} from './sessionRuntime.js';

test('createEmptySessionRuntime has stable defaults', () => {
  const rt = createEmptySessionRuntime();
  assert.equal(rt.running, false);
  assert.deepEqual(rt.assignmentsById, {});
  assert.deepEqual(rt.assignmentOrder, []);
  assert.deepEqual(rt.conversationTabs, [{
    id: 'main', kind: 'main', title: '主对话', closable: false,
  }]);
  assert.equal(rt.activeConversationTab, 'main');
});

test('patchSessionRuntimeMap updates one session only', () => {
  let map = {};
  map = patchSessionRuntimeMap(map, 's1', { draft: 'hello', running: true });
  map = patchSessionRuntimeMap(map, 's2', { draft: 'other' });
  assert.equal(map.s1.draft, 'hello');
  assert.equal(map.s1.running, true);
  assert.equal(map.s2.draft, 'other');
  assert.equal(map.s2.running, false);
});

test('resolveEventSessionId requires EnvelopeV2 session_id', () => {
  assert.equal(resolveEventSessionId({ session_id: 's2' }), 's2');
  assert.equal(resolveEventSessionId({ sessionId: 's2' }), '');
  assert.equal(resolveEventSessionId({ run_id: 'run_a' }), '');
});

test('collectResumeCursors aggregates active runs', () => {
  const map = {
    s1: createEmptySessionRuntime({
      running: true,
      currentRunId: 'run_1',
      runSeq: 5,
      runs: [{ id: 'run_1', status: 'running', lastRunSeq: 4 }],
    }),
    s2: createEmptySessionRuntime({
      runs: [{ id: 'run_2', status: 'waiting_permission', lastRunSeq: 9 }],
    }),
    s3: createEmptySessionRuntime({
      runs: [{ id: 'run_3', status: 'completed', lastRunSeq: 1 }],
    }),
  };
  const cursors = collectResumeCursors(map);
  assert.equal(cursors.run_1, 5);
  assert.equal(cursors.run_2, 9);
  assert.equal(cursors.run_3, undefined);
});

test('collectSessionRunStatus and countActiveRuns', () => {
  const map = {
    s1: createEmptySessionRuntime({ running: true }),
    s2: createEmptySessionRuntime({
      permissions: [{ id: 'p1', status: 'pending' }],
    }),
    s3: createEmptySessionRuntime(),
  };
  const status = collectSessionRunStatus(map);
  assert.equal(status.s1, 'running');
  assert.equal(status.s2, 'waiting_permission');
  assert.equal(status.s3, 'idle');
  assert.equal(countActiveRuns(map), 1);
  assert.equal(getSessionRuntime(map, 'missing').running, false);
});
