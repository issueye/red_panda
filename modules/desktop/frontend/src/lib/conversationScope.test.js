import assert from 'node:assert/strict';
import test from 'node:test';
import {
  extractWorkerScope,
  filterMainMessages,
  filterMainPermissions,
  filterMainTools,
  filterVisibleMessagesAfterCompaction,
  filterWorkerMessages,
  filterWorkerPermissions,
  filterWorkerTools,
  isMainConversationItem,
} from './conversationScope.js';
import { estimateSessionTokens } from './tokenBudget.js';

test('messages covered by the active summary are hidden from conversation views', () => {
  const messages = [
    { id: 'old-user', messageSeq: 1, role: 'user', text: 'old prompt' },
    { id: 'old-reply', messageSeq: 2, role: 'assistant', text: 'old reply' },
    { id: 'tail-user', messageSeq: 3, role: 'user', text: 'kept prompt' },
    { id: 'tail-reply', messageSeq: 4, role: 'assistant', text: 'kept reply' },
    { id: 'live', role: 'assistant', text: 'streaming output' },
    { id: 'notice', role: 'assistant', agent: 'system', text: 'summary complete' },
  ];

  assert.deepEqual(
    filterVisibleMessagesAfterCompaction(messages, { endSeq: 2 }).map((item) => item.id),
    ['tail-user', 'tail-reply', 'live', 'notice'],
  );
  assert.equal(filterVisibleMessagesAfterCompaction(messages, null), messages);
});

test('main conversation keeps only root-facing content', () => {
  const messages = [
    { id: 'user', role: 'user', text: 'go' },
    { id: 'root', workerId: 'worker-01', assignmentId: 'assignment-000001', profileKey: 'root', text: 'plan' },
    { id: 'public-worker-leak', workerId: 'worker-02', assignmentId: 'assignment-000002', text: 'worker draft' },
    { id: 'private', workerId: 'worker-02', visibility: 'worker_private', text: 'draft' },
    { id: 'reviewer', workerId: 'worker-03', profileKey: 'reviewer', text: 'review notes' },
    { id: 'system', agent: 'system', text: '上下文摘要完成' },
  ];
  assert.deepEqual(
    filterMainMessages(messages).map((item) => item.id),
    ['user', 'root', 'system'],
  );

  const tools = [
    { id: 'root_tool', workerId: 'worker-01', assignmentId: 'assignment-000001', profileKey: 'root', name: 'workspace.grep' },
    { id: 'delegate', workerId: 'worker-01', assignmentId: 'assignment-000001', profileKey: 'root', name: 'worker.delegate' },
    { id: 'worker_tool', workerId: 'worker-02', assignmentId: 'assignment-000002', name: 'shell.exec' },
    { id: 'worker_tool_profile', workerId: 'worker-03', profileKey: 'archivist', name: 'workspace.read_file' },
  ];
  assert.deepEqual(
    filterMainTools(tools).map((item) => item.id),
    ['root_tool', 'delegate'],
  );

  const permissions = [
    { id: 'root_perm', workerId: 'worker-01', profileKey: 'root' },
    { id: 'worker_perm', workerId: 'worker-02', assignmentId: 'assignment-000002' },
  ];
  assert.deepEqual(
    filterMainPermissions(permissions).map((item) => item.id),
    ['root_perm'],
  );
});

test('collaborative Worker output does not contribute to main token usage', () => {
  const messages = [
    { id: 'user', role: 'user', text: 'main prompt' },
    { id: 'root', role: 'assistant', profileKey: 'root', text: 'main reply' },
    { id: 'worker', role: 'assistant', profileKey: 'reviewer', text: 'worker output '.repeat(200) },
  ];
  const tools = [
    { id: 'root-tool', profileKey: 'root', name: 'workspace.read_file', output: 'main result' },
    { id: 'worker-tool', profileKey: 'reviewer', name: 'shell.exec', output: 'worker result '.repeat(200) },
  ];

  const mainUsed = estimateSessionTokens(filterMainMessages(messages), '', filterMainTools(tools));
  const expected = estimateSessionTokens(messages.slice(0, 2), '', tools.slice(0, 1));
  const allWorkers = estimateSessionTokens(messages, '', tools);

  assert.equal(mainUsed, expected);
  assert.ok(mainUsed < allWorkers);
});

test('isMainConversationItem classifies root vs collaborative Worker rows', () => {
  assert.equal(isMainConversationItem({ role: 'user', text: 'hi' }), true);
  assert.equal(isMainConversationItem({ profileKey: 'root', workerId: 'worker-01' }), true);
  assert.equal(isMainConversationItem({ workerId: 'worker-02', assignmentId: 'a2' }), false);
  assert.equal(isMainConversationItem({ profileKey: 'reviewer', workerId: 'worker-03' }), false);
  assert.equal(isMainConversationItem({ visibility: 'worker_private', profileKey: 'root' }), false);
});

test('Worker filters prefer Assignment identity and support Worker scope', () => {
  const items = [
    { id: 'a', assignmentId: 'assignment_1', workerId: 'worker-01' },
    { id: 'b', assignmentId: 'assignment_2', workerId: 'worker-01' },
    { id: 'c', assignmentId: 'assignment_3', workerId: 'worker-02' },
    { id: 'd', workerId: 'worker-02' },
  ];
  assert.deepEqual(filterWorkerMessages({ assignmentId: 'assignment_1', workerId: 'worker-02' }, items).map((item) => item.id), ['a', 'd']);
  assert.deepEqual(filterWorkerTools({ workerId: 'worker-01' }, items).map((item) => item.id), ['a', 'b']);
  assert.deepEqual(filterWorkerPermissions({ assignmentId: 'assignment_3' }, items).map((item) => item.id), ['c']);
});

test('extractWorkerScope reads only EnvelopeV2 fields', () => {
  assert.deepEqual(extractWorkerScope({
    assignment_id: 'assignment_9',
    worker: { id: 'worker-03', profile_key: 'reviewer' },
  }), { assignmentId: 'assignment_9', workerId: 'worker-03', profileKey: 'reviewer' });
  assert.deepEqual(extractWorkerScope({ subagent_id: 'legacy' }), {
    assignmentId: '', workerId: '', profileKey: '',
  });
});
