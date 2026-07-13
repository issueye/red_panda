import assert from 'node:assert/strict';
import test from 'node:test';
import { isRunTerminalEvent } from './runEventLifecycle.js';

const envelope = (type) => ({ protocol_version: '2026-07-13', run_id: 'run_1', type });

test('v0.2 finish and error terminate a Run', () => {
  assert.equal(isRunTerminalEvent(envelope('finish')), true);
  assert.equal(isRunTerminalEvent(envelope('error')), true);
  assert.equal(isRunTerminalEvent(envelope('message_delta')), false);
});

test('legacy and Assignment lifecycle events never terminate a Run', () => {
  assert.equal(isRunTerminalEvent({ type: 'finish', root_run_id: 'run_1' }), false);
  assert.equal(isRunTerminalEvent({
    ...envelope('worker_assignment_updated'),
    assignment_id: 'assignment_1',
    payload: { status: 'failed' },
  }), false);
});
