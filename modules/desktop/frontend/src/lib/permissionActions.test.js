import assert from 'node:assert/strict';
import test from 'node:test';
import { projectPermissionDecision } from './permissionActions.js';

test('projectPermissionDecision marks permission resolved and clears global queue', () => {
  const result = projectPermissionDecision({
    permissions: [
      { id: 'p1', status: 'pending', runId: 'run_1' },
      { id: 'p2', status: 'pending', runId: 'run_2' },
    ],
    globalPendingPermissions: [
      { id: 'p1', status: 'pending', runId: 'run_1' },
    ],
    runs: [
      { id: 'run_1', status: 'waiting_permission' },
      { id: 'run_2', status: 'running' },
    ],
    id: 'p1',
    decision: 'approve',
    currentRunId: 'run_fallback',
  });

  assert.equal(result.runId, 'run_1');
  assert.equal(result.permissions[0].status, 'resolved');
  assert.equal(result.permissions[0].decision, 'approve');
  assert.equal(result.permissions[1].status, 'pending');
  assert.equal(result.globalPendingPermissions.length, 0);
  assert.equal(result.runs[0].status, 'running');
  assert.equal(result.runs[1].status, 'running');
  assert.ok(result.runs[0].updatedAt);
});

test('projectPermissionDecision falls back to currentRunId', () => {
  const result = projectPermissionDecision({
    permissions: [],
    globalPendingPermissions: [{ id: 'p9', status: 'pending' }],
    runs: [{ id: 'run_x', status: 'waiting_permission' }],
    id: 'p9',
    decision: 'deny',
    currentRunId: 'run_x',
  });
  assert.equal(result.runId, 'run_x');
  assert.equal(result.runs[0].status, 'running');
});
