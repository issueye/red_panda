import assert from 'node:assert/strict';
import test from 'node:test';
import { isRootTerminalRunEvent } from './runEventLifecycle.js';

test('root finish and error are terminal', () => {
  assert.equal(isRootTerminalRunEvent({ type: 'finish', agent: { role: 'root' } }), true);
  assert.equal(isRootTerminalRunEvent({ type: 'error', agent: { role: 'root' } }), true);
});

test('subagent error does not terminate the root run', () => {
  assert.equal(isRootTerminalRunEvent({
    type: 'error',
    agent: { role: 'subagent', subagent_id: 'worker_1' },
    payload: { status: 'failed' },
  }), false);
  assert.equal(isRootTerminalRunEvent({
    type: 'error',
    payload: { subagent_id: 'worker_1', status: 'failed' },
  }), false);
});
