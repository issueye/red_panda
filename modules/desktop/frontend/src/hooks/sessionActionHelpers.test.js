import test from 'node:test';
import assert from 'node:assert/strict';
import {
  appendMessages,
  applyStartedRunProjection,
  beginRunProjection,
  createSystemMessage,
  createUserMessage,
  normalizeWorkspaceRoot,
} from './sessionActionHelpers.js';

const fixedNow = () => Date.parse('2026-07-15T08:00:00.000Z');

function runtimeFixture() {
  return {
    draft: 'draft',
    running: false,
    currentRunId: 'old-run',
    runSeq: 9,
    runSeqByRun: { 'new-run': 4 },
    messages: [{ id: 'existing', role: 'assistant', text: 'existing' }],
    assignmentsById: { a1: { id: 'a1', status: 'running' } },
    assignmentOrder: ['a1'],
    runs: [{ id: 'old-run', status: 'completed' }],
  };
}

test('message helpers preserve local user and system message shapes', () => {
  assert.deepEqual(createUserMessage('hello', undefined, fixedNow), {
    id: 'user_1784102400000',
    role: 'user',
    createdAt: '2026-07-15T08:00:00.000Z',
    text: 'hello',
  });
  assert.deepEqual(createSystemMessage('cmd', 'done', { includeCreatedAt: true, now: fixedNow }), {
    id: 'cmd_1784102400000',
    role: 'assistant',
    agent: 'system',
    createdAt: '2026-07-15T08:00:00.000Z',
    text: 'done',
  });
});

test('createUserMessage attaches image refs when provided (docs/51 §5.2)', () => {
  const attachments = [{ id: 'att_1', mime: 'image/png' }];
  const message = createUserMessage('look here', attachments, fixedNow);
  assert.equal(message.text, 'look here');
  assert.deepEqual(message.attachments, attachments);
});

test('createUserMessage omits attachments when empty', () => {
  const message = createUserMessage('text only', [], fixedNow);
  assert.equal(message.attachments, undefined);
});

test('appendMessages does not mutate the existing runtime message list', () => {
  const runtime = runtimeFixture();
  const next = appendMessages(runtime, { id: 'next' });

  assert.deepEqual(runtime.messages.map((item) => item.id), ['existing']);
  assert.deepEqual(next.messages.map((item) => item.id), ['existing', 'next']);
});

test('beginRunProjection resets run-specific assignment projection', () => {
  const next = beginRunProjection(runtimeFixture(), 'start this');

  assert.equal(next.draft, '');
  assert.equal(next.running, true);
  assert.equal(next.runSeq, 0);
  assert.deepEqual(next.assignmentsById, {});
  assert.deepEqual(next.assignmentOrder, []);
  assert.equal(next.messages.at(-1).text, 'start this');
});

test('applyStartedRunProjection inserts the normalized active run and restores its cursor', () => {
  const next = applyStartedRunProjection(runtimeFixture(), {
    displayText: 'visible input',
    result: { run_id: 'new-run', runtime_mode: 'react', run_seq: 7 },
    runSettings: { runtimeMode: 'simple' },
    sessionId: 'session-1',
    workspace: { root_path: 'E:\\work' },
  });

  assert.equal(next.currentRunId, 'new-run');
  assert.equal(next.runSeq, 4);
  assert.equal(next.runs[0].id, 'new-run');
  assert.equal(next.runs[0].sessionId, 'session-1');
  assert.equal(next.runs[0].workspaceRoot, 'E:\\work');
  assert.equal(next.runs[0].runtimeMode, 'react');
  assert.deepEqual(next.runs.map((item) => item.id), ['new-run', 'old-run']);
});

test('normalizeWorkspaceRoot matches workspace roots case-insensitively', () => {
  assert.equal(normalizeWorkspaceRoot('E:\\Codes\\Red_Panda'), 'e:/codes/red_panda');
  assert.equal(normalizeWorkspaceRoot(''), '');
});
