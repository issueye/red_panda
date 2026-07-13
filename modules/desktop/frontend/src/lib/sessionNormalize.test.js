import assert from 'node:assert/strict';
import test from 'node:test';
import {
  latestActiveRun,
  latestRootSeq,
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
    content: [{ type: 'text', text: 'hello' }],
  });
  assert.equal(got.text, 'hello');
  assert.equal(got.messageSeq, 3);
});

test('normalizeToolCall and normalizeRun keep status fields', () => {
  const tool = normalizeToolCall({
    id: 't1',
    tool_name: 'workspace.list',
    status: 'completed',
    started_seq: 2,
  });
  assert.equal(tool.name, 'workspace.list');
  assert.equal(tool.rootSeq, 2);

  const run = normalizeRun({
    id: 'r1',
    session_id: 's1',
    status: 'running',
    last_root_seq: 9,
  });
  assert.equal(run.sessionId, 's1');
  assert.equal(run.lastRootSeq, 9);
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

test('latestRootSeq reduces max last_root_seq', () => {
  assert.equal(latestRootSeq([{ last_root_seq: 2 }, { last_root_seq: 7 }], 1), 7);
  assert.equal(latestRootSeq(null, 4), 4);
});
