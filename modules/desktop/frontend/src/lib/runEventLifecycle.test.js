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

// A6: only root terminals should trigger Goal hydrate / strip refresh.
test('goal hydrate trigger is root-only for finish and error', () => {
  assert.equal(isRootTerminalRunEvent({
    type: 'finish',
    agent: { role: 'root' },
    session_id: 's1',
    payload: { status: 'completed', loop_end_reason: 'max_turns' },
  }), true);
  assert.equal(isRootTerminalRunEvent({
    type: 'finish',
    agent: { role: 'subagent', subagent_id: 'analyst_1' },
    session_id: 's1',
    payload: { status: 'completed' },
  }), false);
  assert.equal(isRootTerminalRunEvent({
    type: 'message_delta',
    agent: { role: 'root' },
    session_id: 's1',
  }), false);
});
