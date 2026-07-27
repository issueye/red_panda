import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

/**
 * Pure helpers mirrored from toast defaults for regression without DOM.
 * Keeps duration policy documented and testable.
 */
const DEFAULT_DURATION = {
  success: 2400,
  info: 3200,
  warning: 4200,
  error: 0,
};

function resolveDuration(tone, durationMs) {
  if (durationMs != null) return durationMs;
  return DEFAULT_DURATION[tone] ?? DEFAULT_DURATION.info;
}

function trimToasts(list, maxVisible) {
  if (list.length <= maxVisible) return list;
  return list.slice(list.length - maxVisible);
}

describe('toast duration policy', () => {
  it('uses sticky errors and auto-dismiss success', () => {
    assert.equal(resolveDuration('error'), 0);
    assert.equal(resolveDuration('success'), 2400);
    assert.equal(resolveDuration('warning'), 4200);
    assert.equal(resolveDuration('info', 1000), 1000);
  });

  it('keeps only the newest maxVisible toasts', () => {
    const list = [
      { id: 'a' },
      { id: 'b' },
      { id: 'c' },
      { id: 'd' },
      { id: 'e' },
    ];
    assert.deepEqual(trimToasts(list, 3).map((item) => item.id), ['c', 'd', 'e']);
    assert.deepEqual(trimToasts(list, 10), list);
  });
});
