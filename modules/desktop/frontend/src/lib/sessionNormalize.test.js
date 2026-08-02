import assert from 'node:assert/strict';
import test from 'node:test';
import {
  latestActiveRun,
  latestRunSeq,
  normalizeHistoryMessage,
  normalizePermission,
  normalizeRun,
  normalizeSession,
  normalizeToolCall,
  normalizeWorkspace,
} from './sessionNormalize.js';

test('normalizeSession maps workspace and kind labels', () => {
  const got = normalizeSession({
    id: 's1',
    title: 'Demo',
    kind: 'fork',
    workspace_root: 'E:/ws',
    status: 'active',
  });
  assert.equal(got.id, 's1');
  assert.equal(got.workspaceRoot, 'E:/ws');
  assert.equal(got.kind, 'fork');
  assert.match(got.subtitle, /E:\/ws/);
});

test('normalizeWorkspace unifies root fields', () => {
  assert.equal(normalizeWorkspace(null), null);
  const got = normalizeWorkspace({ id: 'w1', root_path: 'D:/code', name: 'code' });
  assert.equal(got.root, 'D:/code');
  assert.equal(got.root_path, 'D:/code');
});

test('normalizeHistoryMessage extracts first text block', () => {
  const got = normalizeHistoryMessage({
    id: 'm1',
    role: 'assistant',
    seq: 3,
    run_id: 'run_1',
    assignment_id: 'assignment_1',
    worker_id: 'worker-01',
    profile_key: 'reviewer',
    run_seq: 5,
    content: [{ type: 'text', text: 'hello' }],
  });
  assert.equal(got.text, 'hello');
  assert.equal(got.messageSeq, 3);
  assert.equal(got.assignmentId, 'assignment_1');
  assert.equal(got.workerId, 'worker-01');
  assert.equal(got.runSeq, 5);
});

test('normalizeHistoryMessage preserves persisted reasoning role', () => {
  const got = normalizeHistoryMessage({
    id: 'm-reasoning',
    role: 'reasoning',
    seq: 4,
    run_id: 'run_1',
    content: [{ type: 'text', text: 'inspect files' }],
  });
  assert.equal(got.role, 'reasoning');
  assert.equal(got.text, 'inspect files');
});

test('normalizeToolCall and normalizeRun keep status fields', () => {
  const tool = normalizeToolCall({
    id: 't1',
    tool_name: 'workspace.list',
    status: 'completed',
    run_id: 'r1',
    assignment_id: 'assignment_1',
    worker_id: 'worker-01',
    run_seq: 2,
  });
  assert.equal(tool.name, 'workspace.list');
  assert.equal(tool.runSeq, 2);
  assert.equal(tool.assignmentId, 'assignment_1');

  const run = normalizeRun({
    id: 'r1',
    session_id: 's1',
    status: 'running',
    last_run_seq: 9,
  });
  assert.equal(run.sessionId, 's1');
  assert.equal(run.lastRunSeq, 9);
});

test('normalizePermission defaults summary', () => {
  const got = normalizePermission({ id: 'p1', run_id: 'r1' });
  assert.equal(got.status, 'pending');
  assert.match(got.summary, /授权/);
});

test('latestActiveRun picks newest running run', () => {
  const got = latestActiveRun([
    { id: 'a', status: 'completed', updated_at: '2026-01-02T00:00:00Z' },
    { id: 'b', status: 'running', updated_at: '2026-01-01T00:00:00Z' },
    { id: 'c', status: 'waiting_permission', updated_at: '2026-01-03T00:00:00Z' },
  ]);
  assert.equal(got.id, 'c');
});

test('latestRunSeq reduces max last_run_seq', () => {
  assert.equal(latestRunSeq([{ last_run_seq: 2 }, { last_run_seq: 7 }], 1), 7);
  assert.equal(latestRunSeq(null, 4), 4);
});
