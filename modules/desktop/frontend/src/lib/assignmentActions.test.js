import assert from 'node:assert/strict';
import test from 'node:test';
import {
  isCancellableAssignment,
  patchAssignmentInRuntime,
} from './assignmentActions.js';

test('isCancellableAssignment requires id, runId, and active status', () => {
  assert.equal(isCancellableAssignment(null), false);
  assert.equal(isCancellableAssignment({ id: 'a', runId: 'r', status: 'completed' }), false);
  assert.equal(isCancellableAssignment({ id: 'a', runId: 'r', status: 'running' }), true);
  assert.equal(isCancellableAssignment({ id: 'a', runId: 'r', status: 'waiting_permission' }), true);
});

test('patchAssignmentInRuntime merges one assignment', () => {
  const runtime = {
    assignmentsById: {
      a1: { id: 'a1', status: 'running', summary: '' },
      a2: { id: 'a2', status: 'queued', summary: '' },
    },
  };
  const next = patchAssignmentInRuntime(runtime, { id: 'a1', status: 'running' }, {
    status: 'cancelling',
    summary: '已请求取消',
  });
  assert.equal(next.assignmentsById.a1.status, 'cancelling');
  assert.equal(next.assignmentsById.a1.summary, '已请求取消');
  assert.equal(next.assignmentsById.a2.status, 'queued');
});
