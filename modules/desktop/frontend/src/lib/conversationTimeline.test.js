import assert from 'node:assert/strict';
import test from 'node:test';
import { buildConversationTimeline } from './conversationTimeline.js';

test('buildConversationTimeline interleaves one run by event sequence', () => {
  const timeline = buildConversationTimeline(
    [
      { id: 'user', role: 'user', rootSeq: 1, text: 'start' },
      { id: 'assistant', role: 'assistant', rootSeq: 4, text: 'done' },
    ],
    [
      { id: 'read', rootRunId: 'run_1', startedSeq: 2 },
      { id: 'shell', rootRunId: 'run_1', startedSeq: 3 },
    ],
    [],
  );

  assert.deepEqual(timeline.map((item) => item.value.id), ['user', 'read', 'shell', 'assistant']);
});

test('buildConversationTimeline uses timestamps across separate runs', () => {
  const timeline = buildConversationTimeline(
    [
      { id: 'first-message', runId: 'run_1', createdAt: '2026-07-10T08:00:00.000Z' },
      { id: 'second-message', runId: 'run_2', createdAt: '2026-07-10T08:10:00.000Z' },
    ],
    [
      { id: 'first-tool', rootRunId: 'run_1', startedSeq: 9, startedAt: '2026-07-10T08:01:00.000Z' },
      { id: 'second-tool', rootRunId: 'run_2', startedSeq: 1, startedAt: '2026-07-10T08:11:00.000Z' },
    ],
    [],
  );

  assert.deepEqual(
    timeline.map((item) => item.value.id),
    ['first-message', 'first-tool', 'second-message', 'second-tool'],
  );
});
