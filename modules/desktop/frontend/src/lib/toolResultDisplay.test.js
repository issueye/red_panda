import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildToolOutputSummary,
  displayToolOutput,
  isSubagentToolFallback,
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

test('isSubagentToolFallback only compacts subagent recovery messages', () => {
  const text = '根据工具执行结果整理如下：\n\n### Read file\nlarge output';
  assert.equal(isSubagentToolFallback({ role: 'assistant', agentRole: 'subagent', text }), true);
  assert.equal(isSubagentToolFallback({ role: 'assistant', agent: 'root', text }), false);
});
