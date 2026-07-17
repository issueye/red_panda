import assert from 'node:assert/strict';
import test from 'node:test';
import {
  CONTEXT_AUTO_COMPACT_RATIO,
  MIN_VISIBLE_RATIO,
  SOFT_CONTEXT_BUDGET,
  coveredCountForKeepTailTurns,
  estimateEffectiveSessionTokens,
  estimateSessionTokens,
  estimateTextTokens,
  formatTokenCount,
  ringFillRatio,
  selectEffectiveMessages,
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

test('effective estimate keeps unsequenced live messages after compact (no ring freeze at 0)', () => {
  const messages = [
    { role: 'user', text: 'old '.repeat(200) },
    { role: 'assistant', text: 'old answer '.repeat(200) },
    { role: 'user', text: 'recent question with enough tokens '.repeat(20) },
    { role: 'assistant', text: 'recent answer with enough tokens '.repeat(20) },
    { role: 'user', text: 'post compact turn '.repeat(30) },
    { role: 'assistant', text: 'post compact reply '.repeat(30) },
  ];
  // Bug: filtering only by messageSeq drops every live row (seq missing → 0).
  const broken = messages.filter((message) => (Number(message?.messageSeq) || 0) > 10);
  assert.equal(broken.length, 0);

  const coveredCount = coveredCountForKeepTailTurns(messages.slice(0, 4), 1);
  const effective = estimateEffectiveSessionTokens(messages, '', [], {
    endSeq: 10,
    coveredCount,
    summary: { summary: 'short summary of older turns' },
  });
  assert.ok(effective > 50, `expected post-compact live tokens to count, got ${effective}`);

  const selected = selectEffectiveMessages(messages, { endSeq: 10, coveredCount });
  assert.ok(selected.some((item) => String(item.text || '').includes('post compact')));
});

test('coveredCountForKeepTailTurns keeps the last N user-led rounds', () => {
  const messages = [
    { role: 'user', text: 'u1' },
    { role: 'assistant', text: 'a1' },
    { role: 'user', text: 'u2' },
    { role: 'assistant', text: 'a2' },
    { role: 'user', text: 'u3' },
    { role: 'assistant', text: 'a3' },
  ];
  assert.equal(coveredCountForKeepTailTurns(messages, 2), 2);
  assert.equal(coveredCountForKeepTailTurns(messages, 3), 0);
  assert.equal(coveredCountForKeepTailTurns(messages, 1), 4);
});

test('selectEffectiveMessages uses messageSeq when present and keeps live unsequenced tail', () => {
  const messages = [
    { messageSeq: 1, text: 'old' },
    { messageSeq: 2, text: 'old answer' },
    { messageSeq: 3, text: 'recent' },
    { role: 'assistant', text: 'streaming live', id: 'evt_1' },
  ];
  const selected = selectEffectiveMessages(messages, { endSeq: 2, coveredCount: 3 });
  assert.deepEqual(selected.map((item) => item.text), ['recent', 'streaming live']);
});

test('tokenBudgetState marks 80% auto-compact threshold', () => {
  assert.equal(CONTEXT_AUTO_COMPACT_RATIO, 0.8);
  const disabled = tokenBudgetState(1000, 0);
  assert.equal(disabled.enabled, false);
  assert.equal(disabled.autoCompact, false);
  assert.equal(disabled.softBudget, true);
  assert.ok(disabled.displayRatio > 0);

  const mid = tokenBudgetState(79, 100);
  assert.equal(mid.enabled, true);
  assert.equal(mid.autoCompact, false);
  assert.ok(Math.abs(mid.ratio - 0.79) < 1e-9);
  assert.ok(mid.displayRatio >= MIN_VISIBLE_RATIO);

  const hot = tokenBudgetState(80, 100);
  assert.equal(hot.autoCompact, true);
  assert.equal(hot.ratio, 0.8);
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
