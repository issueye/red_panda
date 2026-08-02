import assert from 'node:assert/strict';
import test from 'node:test';
import { estimateEffectiveSessionTokens, estimateSessionTokens } from './tokenBudget.js';
import {
  mergeSessionHistoryMessages,
  replaceHistoryWithRunStreams,
} from './sessionMessageMerge.js';

const identity = {
  role: 'assistant',
  runId: 'run-1',
  assignmentId: 'assignment-1',
  workerId: 'worker-01',
  profileKey: 'root',
  visibility: 'run_public',
};

test('drops streamed event rows already represented by refreshed history', () => {
  const previous = [
    { id: 'msg-1', messageSeq: 1, role: 'user', text: 'question' },
    { id: 'msg-2', messageSeq: 2, ...identity, text: 'saved prefix ' },
    { id: 'evt-3', ...identity, text: 'live part one ' },
    { id: 'evt-5', ...identity, text: 'live part two' },
  ];
  const history = [
    previous[0],
    { id: 'msg-2', messageSeq: 2, ...identity, text: 'saved prefix live part one live part two' },
  ];

  const merged = mergeSessionHistoryMessages(previous, history);

  assert.deepEqual(merged, history);
});

test('keeps only a streamed suffix that is not persisted yet', () => {
  const previous = [
    { id: 'msg-1', messageSeq: 1, ...identity, text: 'saved prefix ' },
    { id: 'evt-2', ...identity, text: 'persisted delta ' },
    { id: 'evt-4', ...identity, text: 'still live' },
  ];
  const history = [
    { id: 'msg-1', messageSeq: 1, ...identity, text: 'saved prefix persisted delta ' },
  ];

  const merged = mergeSessionHistoryMessages(previous, history);

  assert.equal(merged.length, 2);
  assert.equal(merged[1].id, 'evt-4');
  assert.equal(merged[1].text, 'still live');
});

test('keeps repeated live text when refreshed history did not advance', () => {
  const saved = { id: 'msg-1', messageSeq: 1, ...identity, text: 'same reply' };
  const repeated = { id: 'evt-2', ...identity, text: 'same reply' };

  assert.deepEqual(mergeSessionHistoryMessages([saved, repeated], [saved]), [saved, repeated]);
});

test('drops live reasoning once refreshed history persists it', () => {
  const reasoningIdentity = { ...identity, role: 'reasoning' };
  const previous = [
    { id: 'msg-r1', messageSeq: 1, ...reasoningIdentity, text: 'inspect ' },
    { id: 'evt-r2', ...reasoningIdentity, text: 'files' },
  ];
  const history = [
    { id: 'msg-r1', messageSeq: 1, ...reasoningIdentity, text: 'inspect files' },
  ];

  assert.deepEqual(mergeSessionHistoryMessages(previous, history), history);
});

test('authoritative run streams replace reordered persisted output', () => {
  const history = [
    { id: 'user-1', messageSeq: 1, role: 'user', runId: 'run-1', text: 'question' },
    { id: 'old-reasoning', messageSeq: 2, ...identity, role: 'reasoning', text: 'before toolafter tool' },
    { id: 'old-answer', messageSeq: 3, ...identity, text: 'answer' },
    { id: 'legacy-answer', messageSeq: 4, ...identity, runId: 'run-legacy', text: 'legacy' },
  ];
  const streams = [
    { id: 'evt-1', ...identity, role: 'reasoning', runSeq: 1, text: 'before tool' },
    { id: 'evt-4', ...identity, role: 'reasoning', runSeq: 4, text: 'after tool' },
    { id: 'evt-6', ...identity, runSeq: 6, text: 'answer' },
  ];

  const merged = replaceHistoryWithRunStreams(history, streams);
  assert.deepEqual(merged.map((message) => message.id), [
    'user-1', 'legacy-answer', 'evt-1', 'evt-4', 'evt-6',
  ]);
});

test('authoritative streams fall back to persisted output on text mismatch', () => {
  const history = [
    { id: 'saved', messageSeq: 1, ...identity, text: 'complete answer' },
  ];
  const streams = [
    { id: 'partial-event', ...identity, runSeq: 2, text: 'partial' },
  ];

  assert.deepEqual(replaceHistoryWithRunStreams(history, streams), history);
});

test('trims a partially persisted streamed row', () => {
  const previous = [
    { id: 'msg-1', messageSeq: 1, ...identity, text: 'saved ' },
    { id: 'evt-2', ...identity, text: 'partly persisted and live' },
  ];
  const history = [
    { id: 'msg-1', messageSeq: 1, ...identity, text: 'saved partly persisted ' },
  ];

  const merged = mergeSessionHistoryMessages(previous, history);

  assert.equal(merged.length, 2);
  assert.equal(merged[1].id, 'evt-2');
  assert.equal(merged[1].text, 'and live');
});

test('reconciles a newly persisted optimistic user message by occurrence', () => {
  const previous = [
    { id: 'msg-old', messageSeq: 1, role: 'user', text: 'repeat' },
    { id: 'user_2', role: 'user', text: 'repeat' },
  ];
  const history = [
    previous[0],
    { id: 'msg-new', messageSeq: 2, role: 'user', text: 'repeat' },
  ];

  assert.deepEqual(mergeSessionHistoryMessages(previous, history), history);
});

test('preserves UI-only system notices for display', () => {
  const notice = { id: 'compact-resume', role: 'assistant', agent: 'system', text: 'summary complete' };
  const history = [{ id: 'msg-1', messageSeq: 1, role: 'user', text: 'question' }];

  assert.deepEqual(mergeSessionHistoryMessages([...history, notice], history), [...history, notice]);
});

test('compaction lowers estimated usage and later live output grows it again', () => {
  const oldQuestion = { id: 'msg-1', messageSeq: 1, role: 'user', text: 'old question '.repeat(200) };
  const previous = [
    oldQuestion,
    { id: 'msg-2', messageSeq: 2, ...identity, text: 'old answer '.repeat(200) },
    { id: 'evt-3', ...identity, text: 'recent answer '.repeat(30) },
  ];
  const history = [
    oldQuestion,
    { id: 'msg-2', messageSeq: 2, ...identity, text: `${'old answer '.repeat(200)}${'recent answer '.repeat(30)}` },
  ];
  const merged = mergeSessionHistoryMessages(previous, history);
  const before = estimateSessionTokens(previous);
  const compacted = estimateEffectiveSessionTokens(merged, '', [], {
    endSeq: 1,
    summary: { summary: 'short summary' },
  });
  const withNewOutput = estimateEffectiveSessionTokens([
    ...merged,
    { id: 'evt-new', ...identity, text: 'brand new live output '.repeat(20) },
  ], '', [], {
    endSeq: 1,
    summary: { summary: 'short summary' },
  });

  assert.ok(compacted < before);
  assert.ok(withNewOutput > compacted);
});
