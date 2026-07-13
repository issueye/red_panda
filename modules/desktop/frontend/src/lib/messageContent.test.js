import assert from 'node:assert/strict';
import test from 'node:test';

import {
  messageHasToolCallMarkup,
  normalizeToolName,
  parseMessageContent,
  parseToolCallBody,
  toolDisplayName,
  toolItemFromMessageSegment,
} from './messageContent.js';

test('normalizeToolName converts double underscore and shorthand', () => {
  assert.equal(normalizeToolName('workspace__read'), 'workspace.read_file');
  assert.equal(normalizeToolName('workspace.read'), 'workspace.read_file');
  assert.equal(normalizeToolName('workspace.write_file'), 'workspace.write_file');
  assert.equal(normalizeToolName('shell.exec'), 'shell.exec');
});

test('parseToolCallBody handles function= and parameter= form from screenshot', () => {
  const body = `
    <function=workspace__read>
    <parameter=path> ops-governance-platform/product-design-doc.md </parameter>
    </function>
  `;
  const parsed = parseToolCallBody(body);
  assert.equal(parsed.name, 'workspace.read_file');
  assert.equal(parsed.arguments.path, 'ops-governance-platform/product-design-doc.md');
});

test('parseToolCallBody handles name attributes', () => {
  const body = `
    <function name="workspace.list">
      <parameter name="path">.</parameter>
    </function>
  `;
  const parsed = parseToolCallBody(body);
  assert.equal(parsed.name, 'workspace.list');
  assert.equal(parsed.arguments.path, '.');
});

test('parseMessageContent splits surrounding text and tool calls', () => {
  const source = [
    '先读取产品文档。',
    '<tool_call> <function=workspace__read> <parameter=path> ops-governance-platform/product-design-doc.md </parameter>',
    '</function> </tool_call>',
    '然后给出目录建议。',
  ].join('\n');

  const segments = parseMessageContent(source);
  assert.equal(segments.length, 3);
  assert.equal(segments[0].type, 'text');
  assert.match(segments[0].text, /先读取产品文档/);
  assert.equal(segments[1].type, 'tool_call');
  assert.equal(segments[1].name, 'workspace.read_file');
  assert.equal(segments[1].arguments.path, 'ops-governance-platform/product-design-doc.md');
  assert.equal(segments[1].complete, true);
  assert.equal(segments[2].type, 'text');
  assert.match(segments[2].text, /然后给出目录建议/);
});

test('parseMessageContent handles incomplete streaming tool_call', () => {
  const source = '分析中\n<tool_call>\n<function=workspace__list>\n<parameter=path>.';
  const segments = parseMessageContent(source);
  assert.equal(segments.length, 2);
  assert.equal(segments[0].type, 'text');
  assert.equal(segments[1].type, 'tool_call');
  assert.equal(segments[1].complete, false);
  assert.equal(segments[1].name, 'workspace.list');
});

test('message that is only a tool call yields a single tool segment', () => {
  const source =
    '<tool_call> <function=workspace__read> <parameter=path> ops-governance-platform/product-design-doc.md </parameter>\n</function> </tool_call>';
  const segments = parseMessageContent(source);
  assert.equal(segments.length, 1);
  assert.equal(segments[0].type, 'tool_call');
  assert.equal(messageHasToolCallMarkup(source), true);
});

test('toolItemFromMessageSegment builds card payload', () => {
  const item = toolItemFromMessageSegment(
    {
      type: 'tool_call',
      name: 'workspace.read_file',
      arguments: { path: 'a.md' },
      complete: true,
    },
    { id: 'msg_1', runId: 'run_1', assignmentId: 'assignment_1', workerId: 'worker-01', runSeq: 7 },
    0,
  );
  assert.equal(item.id, 'msg_1_inline_tool_0');
  assert.equal(item.displayName, 'Read file');
  assert.equal(item.name, 'workspace.read_file');
  assert.equal(item.status, 'completed');
  assert.equal(item.arguments.path, 'a.md');
  assert.equal(item.assignmentId, 'assignment_1');
  assert.equal(item.workerId, 'worker-01');
  assert.equal(item.runSeq, 7);
  assert.equal(toolDisplayName('workspace.list'), 'List files');
});

test('Worker communication tools have readable labels', () => {
  assert.equal(toolDisplayName('worker.delegate'), 'Delegate work');
  assert.equal(toolDisplayName('worker.send'), 'Send to Worker');
  assert.equal(toolDisplayName('worker.receive'), 'Receive from Worker');
});
