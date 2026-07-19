import assert from 'node:assert/strict';
import test from 'node:test';
import {
  closeConversationTabState,
  openWorkerConversationState,
} from './conversationTabs.js';

test('openWorkerConversationState adds a worker tab and focuses it', () => {
  const runtime = {
    conversationTabs: [{ id: 'main', kind: 'main', title: '主对话', closable: false }],
    activeConversationTab: 'main',
  };
  const next = openWorkerConversationState(runtime, {
    id: 'asg1',
    workerId: 'w1',
    profileKey: 'implementer',
    runId: 'run1',
    task: 'do work',
    status: 'running',
  });
  assert.equal(next.activeConversationTab, 'worker:asg1');
  assert.equal(next.conversationTabs.length, 2);
  assert.equal(next.conversationTabs[1].assignmentId, 'asg1');
  assert.equal(next.conversationTabs[1].kind, 'worker');
});

test('openWorkerConversationState refreshes existing worker tab', () => {
  const runtime = {
    conversationTabs: [
      { id: 'main', kind: 'main', title: '主对话', closable: false },
      {
        id: 'worker:asg1',
        kind: 'worker',
        assignmentId: 'asg1',
        task: 'old',
        status: 'queued',
        closable: true,
      },
    ],
    activeConversationTab: 'main',
  };
  const next = openWorkerConversationState(runtime, {
    id: 'asg1',
    task: 'new task',
    status: 'running',
  });
  assert.equal(next.conversationTabs.length, 2);
  assert.equal(next.conversationTabs[1].task, 'new task');
  assert.equal(next.conversationTabs[1].status, 'running');
  assert.equal(next.activeConversationTab, 'worker:asg1');
});

test('closeConversationTabState returns to main when closing active tab', () => {
  const runtime = {
    conversationTabs: [
      { id: 'main', kind: 'main', closable: false },
      { id: 'worker:asg1', kind: 'worker', closable: true },
    ],
    activeConversationTab: 'worker:asg1',
  };
  const next = closeConversationTabState(runtime, 'worker:asg1');
  assert.equal(next.conversationTabs.length, 1);
  assert.equal(next.activeConversationTab, 'main');
});

test('closeConversationTabState ignores main', () => {
  const runtime = {
    conversationTabs: [{ id: 'main', kind: 'main', closable: false }],
    activeConversationTab: 'main',
  };
  const next = closeConversationTabState(runtime, 'main');
  assert.equal(next, runtime);
});
