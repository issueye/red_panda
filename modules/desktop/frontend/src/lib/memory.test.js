import assert from 'node:assert/strict';
import test from 'node:test';

import {
  memoryCreatePayload,
  memoryDraftFrom,
  memoryListQuery,
  memoryPreviewPayload,
  memoryUpdatePayload,
  normalizeMemoryRecord,
} from './memory.js';

test('normalizeMemoryRecord maps Gateway fields and defaults', () => {
  const item = normalizeMemoryRecord({
    id: 'mem_1',
    scope: 'session',
    kind: 'decision',
    title: '',
    content: 'Keep tests focused.',
    session_id: 'session_1',
  });

  assert.equal(item.id, 'mem_1');
  assert.equal(item.scope, 'session');
  assert.equal(item.kind, 'decision');
  assert.equal(item.status, 'active');
  assert.equal(item.title, 'mem_1');
  assert.equal(item.content, 'Keep tests focused.');
  assert.equal(item.sessionId, 'session_1');
});

test('memory payload helpers use Gateway field names', () => {
  assert.deepEqual(memoryCreatePayload({
    scope: 'project',
    kind: 'fact',
    title: 'Build',
    content: 'Use npm test.',
    confidence: 'high',
  }, {
    workspaceRoot: 'D:/workspace',
    sessionId: 'session_1',
  }), {
    scope: 'project',
    kind: 'fact',
    status: 'active',
    title: 'Build',
    content: 'Use npm test.',
    confidence: 'high',
    workspace_root: 'D:/workspace',
    source: 'user',
  });

  assert.deepEqual(memoryUpdatePayload({
    scope: 'session',
    kind: 'task',
    status: 'disabled',
    title: 'Next',
    content: 'Do this later.',
    confidence: 'medium',
  }), {
    scope: 'session',
    kind: 'task',
    status: 'disabled',
    title: 'Next',
    content: 'Do this later.',
    confidence: 'medium',
  });

  assert.deepEqual(memoryPreviewPayload({
    workspaceRoot: 'D:/workspace',
    sessionId: 'session_1',
    input: 'hello',
  }), {
    workspace_root: 'D:/workspace',
    session_id: 'session_1',
    input: 'hello',
  });
});

test('memoryDraftFrom and memoryListQuery keep scope-specific filters explicit', () => {
  assert.deepEqual(memoryDraftFrom(null), {
    scope: 'project',
    kind: 'fact',
    status: 'active',
    title: '',
    content: '',
    confidence: 'medium',
  });

  assert.equal(
    memoryListQuery({ scope: 'project', status: 'disabled', workspaceRoot: 'D:/workspace' }),
    '/api/v1/memory?scope=project&status=disabled&workspace_root=D%3A%2Fworkspace',
  );
  assert.equal(
    memoryListQuery({ scope: 'session', status: 'active', sessionId: 'session_1' }),
    '/api/v1/memory?scope=session&session_id=session_1',
  );
});
