import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createEmptySessionRuntime,
  pruneIdleSessionRuntimes,
} from './sessionRuntime.js';

test('pruneIdleSessionRuntimes keeps focused and running sessions', () => {
  const map = {
    keep: { ...createEmptySessionRuntime(), lastTouchedAt: 1 },
    run: { ...createEmptySessionRuntime(), running: true, lastTouchedAt: 1 },
    old1: { ...createEmptySessionRuntime(), lastTouchedAt: 10 },
    old2: { ...createEmptySessionRuntime(), lastTouchedAt: 20 },
    old3: { ...createEmptySessionRuntime(), lastTouchedAt: 5 },
  };
  const next = pruneIdleSessionRuntimes(map, { keepSessionId: 'keep', maxIdle: 1 });
  assert.ok(next.keep);
  assert.ok(next.run);
  // Only one idle kept — the most recently touched among old*.
  const idleKeys = Object.keys(next).filter((id) => id !== 'keep' && id !== 'run');
  assert.equal(idleKeys.length, 1);
  assert.equal(idleKeys[0], 'old2');
});

test('pruneIdleSessionRuntimes is no-op under maxIdle', () => {
  const map = {
    a: { ...createEmptySessionRuntime(), lastTouchedAt: 1 },
    b: { ...createEmptySessionRuntime(), lastTouchedAt: 2 },
  };
  const next = pruneIdleSessionRuntimes(map, { keepSessionId: 'a', maxIdle: 5 });
  assert.deepEqual(Object.keys(next).sort(), ['a', 'b']);
});
