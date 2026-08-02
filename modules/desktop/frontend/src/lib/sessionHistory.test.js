import assert from 'node:assert/strict';
import test from 'node:test';

import { loadAllSessionHistory, loadAllSessions, loadSessionBootstrap } from './sessionHistory.js';

test('loadAllSessions follows offset pages until complete', async () => {
  const urls = [];
  const request = async (url) => {
    urls.push(url);
    if (urls.length === 1) return { items: [{ id: 's2' }], has_more: true, next_offset: 1 };
    return { items: [{ id: 's1' }], has_more: false };
  };
  const sessions = await loadAllSessions(request);
  assert.deepEqual(sessions.map((item) => item.id), ['s2', 's1']);
  assert.match(urls[1], /offset=1/);
});

test('loadAllSessionHistory follows sequence pages until complete', async () => {
  const urls = [];
  const request = async (url) => {
    urls.push(url);
    if (urls.length === 1) {
      return { items: [{ seq: 1 }, { seq: 2 }], has_more: true, next_after_seq: 2 };
    }
    return { items: [{ seq: 3 }], has_more: false };
  };
  const history = await loadAllSessionHistory('session 1', request);
  assert.deepEqual(history.map((item) => item.seq), [1, 2, 3]);
  assert.match(urls[0], /session%201\/history\?after_seq=0/);
  assert.match(urls[1], /after_seq=2/);
});

test('loadAllSessionHistory rejects a stalled cursor', async () => {
  await assert.rejects(
    loadAllSessionHistory('s1', async () => ({ items: [], has_more: true, next_after_seq: 0 })),
    /cursor did not advance/,
  );
});

test('loadSessionBootstrap carries authoritative run streams', async () => {
  const result = await loadSessionBootstrap('s1', async () => ({
    history: { items: [{ id: 'message-1' }] },
    streams: [{ id: 'event-1', role: 'reasoning', run_seq: 2 }],
    runs: [],
    context: {},
  }));
  assert.equal(result.history[0].id, 'message-1');
  assert.equal(result.streams[0].id, 'event-1');
});
