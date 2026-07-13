import assert from 'node:assert/strict';
import test from 'node:test';
import {
  extractWorkerScope,
  filterMainMessages,
  filterMainPermissions,
  filterMainTools,
  filterWorkerMessages,
  filterWorkerPermissions,
  filterWorkerTools,
} from './conversationScope.js';

test('main conversation keeps public Run content only', () => {
  const messages = [
    { id: 'public', workerId: 'worker-01', text: 'report' },
    { id: 'private', workerId: 'worker-02', visibility: 'worker_private', text: 'draft' },
  ];
  assert.deepEqual(filterMainMessages(messages).map((item) => item.id), ['public']);
  assert.equal(filterMainTools([{ id: 'tool_1', workerId: 'worker-01' }]).length, 1);
  assert.equal(filterMainPermissions([{ id: 'perm_1', workerId: 'worker-01' }]).length, 1);
});

test('Worker filters prefer Assignment identity and support Worker scope', () => {
  const items = [
    { id: 'a', assignmentId: 'assignment_1', workerId: 'worker-01' },
    { id: 'b', assignmentId: 'assignment_2', workerId: 'worker-01' },
    { id: 'c', assignmentId: 'assignment_3', workerId: 'worker-02' },
  ];
  assert.deepEqual(filterWorkerMessages({ assignmentId: 'assignment_1', workerId: 'worker-02' }, items).map((item) => item.id), ['a', 'c']);
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
