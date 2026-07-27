import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildToolOutputSummary,
  displayToolOutput,
  isWorkerToolFallback,
} from './toolResultDisplay.js';

function envelope(overrides = {}) {
  return JSON.stringify({
    schema: 'red_panda.tool_result.v1',
    tool: 'workspace.read_file',
    status: 'completed',
    ok: true,
    text: 'file content',
    meta: {},
    ...overrides,
  });
}

test('displayToolOutput unwraps standardized success text', () => {
  assert.equal(displayToolOutput(envelope()), 'file content');
  assert.equal(buildToolOutputSummary(envelope()), 'file content');
});

test('displayToolOutput does not repeat a standardized failure error', () => {
  const output = envelope({ status: 'failed', ok: false, text: 'file not found', error: 'file not found' });
  assert.equal(displayToolOutput(output, 'file not found'), '');
});

test('displayToolOutput recovers diagnostics from historical failure envelopes', () => {
  const output = envelope({
    status: 'failed',
    ok: false,
    text: 'exit status 1',
    error: 'exit status 1',
    data: { raw: 'compile.go:12: undefined: value' },
  });
  assert.equal(displayToolOutput(output, 'exit status 1'), 'compile.go:12: undefined: value');
});

test('isWorkerToolFallback only compacts public Assignment recovery messages', () => {
  const text = '根据工具执行结果整理如下：\n\n### Read file\nlarge output';
  assert.equal(isWorkerToolFallback({ role: 'assistant', assignmentId: 'assignment_1', workerId: 'worker-01', profileKey: 'reviewer', text }), true);
  assert.equal(isWorkerToolFallback({ role: 'assistant', assignmentId: 'assignment_root', workerId: 'worker-03', profileKey: 'root', text }), false);
  assert.equal(isWorkerToolFallback({ role: 'assistant', workerId: 'worker-01', visibility: 'worker_private', text }), false);
  assert.equal(isWorkerToolFallback({ role: 'assistant', text }), false);
});
