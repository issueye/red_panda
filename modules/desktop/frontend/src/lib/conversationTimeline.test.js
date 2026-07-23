import assert from 'node:assert/strict';
import test from 'node:test';
import {
  buildConversationTimeline,
  buildToolCallIndexMap,
  compareToolCallOrder,
} from './conversationTimeline.js';

test('buildConversationTimeline interleaves one run by event sequence', () => {
  const timeline = buildConversationTimeline(
    [
      { id: 'user', role: 'user', runId: 'run_1', runSeq: 1, text: 'start' },
      { id: 'assistant', role: 'assistant', runId: 'run_1', runSeq: 4, text: 'done' },
    ],
    [
      { id: 'read', runId: 'run_1', runSeq: 2 },
      { id: 'shell', runId: 'run_1', runSeq: 3 },
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
      { id: 'first-tool', runId: 'run_1', runSeq: 9, startedAt: '2026-07-10T08:01:00.000Z' },
      { id: 'second-tool', runId: 'run_2', runSeq: 1, startedAt: '2026-07-10T08:11:00.000Z' },
    ],
    [],
  );

  assert.deepEqual(
    timeline.map((item) => item.value.id),
    ['first-message', 'first-tool', 'second-message', 'second-tool'],
  );
});

test('buildToolCallIndexMap numbers tools by startedSeq then startedAt', () => {
  const map = buildToolCallIndexMap([
    { id: 'later', runSeq: 5, startedAt: '2026-07-10T08:02:00.000Z' },
    { id: 'earlier', startedSeq: 2, startedAt: '2026-07-10T08:01:00.000Z' },
    { id: 'mid', runSeq: 3, startedAt: '2026-07-10T08:01:30.000Z' },
  ]);
  assert.equal(map.get('earlier'), 1);
  assert.equal(map.get('mid'), 2);
  assert.equal(map.get('later'), 3);
  assert.equal(map.size, 3);
});

test('compareToolCallOrder prefers startedSeq over id', () => {
  const order = [
    { id: 'b', startedSeq: 2 },
    { id: 'a', startedSeq: 1 },
  ].sort(compareToolCallOrder);
  assert.deepEqual(order.map((item) => item.id), ['a', 'b']);
});
