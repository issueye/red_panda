import assert from 'node:assert/strict';
import test from 'node:test';
import {
  CONTEXT_AUTO_COMPACT_RATIO,
  MIN_VISIBLE_RATIO,
  SOFT_CONTEXT_BUDGET,
  estimateEffectiveSessionTokens,
  estimateSessionTokens,
  estimateTextTokens,
  formatTokenCount,
  ringFillRatio,
  tokenBudgetState,
} from './tokenBudget.js';

test('estimateTextTokens treats CJK denser than latin', () => {
  const latin = estimateTextTokens('abcd'); // ~1
  const cjk = estimateTextTokens('中文测试'); // ~4
  assert.ok(cjk > latin);
  assert.equal(estimateTextTokens(''), 0);
});

test('estimateSessionTokens includes draft, tools, and message overhead', () => {
  const used = estimateSessionTokens([{ text: 'hello world' }], 'more', [
    { name: 'workspace.read_file', output: 'file contents here' },
  ]);
  assert.ok(used > estimateTextTokens('hello world'));
  assert.ok(used > estimateSessionTokens([{ text: 'hello world' }], 'more'));
});

test('effective estimate replaces covered history with summary without deleting messages', () => {
  const messages = [
    { messageSeq: 1, text: 'old '.repeat(200) },
    { messageSeq: 2, text: 'old answer '.repeat(200) },
    { messageSeq: 3, text: 'recent question' },
    { messageSeq: 4, text: 'recent answer' },
  ];
  const full = estimateSessionTokens(messages);
  const effective = estimateEffectiveSessionTokens(messages, '', [], {
    endSeq: 2,
    summary: { summary: 'short summary' },
  });
  assert.equal(messages.length, 4);
  assert.ok(effective < full);
});

test('tokenBudgetState marks 90% auto-compact threshold', () => {
  assert.equal(CONTEXT_AUTO_COMPACT_RATIO, 0.9);
  const disabled = tokenBudgetState(1000, 0);
  assert.equal(disabled.enabled, false);
  assert.equal(disabled.autoCompact, false);
  assert.equal(disabled.softBudget, true);
  assert.ok(disabled.displayRatio > 0);

  const mid = tokenBudgetState(80, 100);
  assert.equal(mid.enabled, true);
  assert.equal(mid.autoCompact, false);
  assert.ok(Math.abs(mid.ratio - 0.8) < 1e-9);
  assert.ok(mid.displayRatio >= MIN_VISIBLE_RATIO);

  const hot = tokenBudgetState(90, 100);
  assert.equal(hot.autoCompact, true);
  assert.equal(hot.ratio, 0.9);
});

test('ringFillRatio keeps tiny usage visible', () => {
  assert.equal(ringFillRatio(0, 128000), 0);
  assert.ok(ringFillRatio(200, 128000) >= MIN_VISIBLE_RATIO);
  assert.equal(ringFillRatio(128000, 128000), 1);
});

test('soft budget uses soft ceiling when max unset', () => {
  const state = tokenBudgetState(SOFT_CONTEXT_BUDGET / 2, 0);
  assert.equal(state.enabled, false);
  assert.ok(state.displayRatio > 0.4 && state.displayRatio < 0.6);
});

test('formatTokenCount short labels', () => {
  assert.equal(formatTokenCount(120), '120');
  assert.equal(formatTokenCount(1500), '1.5k');
  assert.equal(formatTokenCount(12000), '12k');
});
