import assert from 'node:assert/strict';
import test from 'node:test';
import {
  extractAgentScope,
  filterMainMessages,
  filterMainTools,
  filterSubagentMessages,
  filterSubagentTools,
} from './conversationScope.js';

test('filterMainMessages keeps user and root assistants only', () => {
  const items = filterMainMessages([
    { id: '1', role: 'user', text: 'hi' },
    { id: '2', role: 'assistant', agent: 'root', text: 'ok' },
    { id: '3', role: 'assistant', agent: 'desktop', agentRole: 'subagent', subagentId: 's1', text: 'sub' },
  ]);
  assert.deepEqual(items.map((item) => item.id), ['1', '2']);
});

test('filterSubagentMessages matches by subagent id and run id', () => {
  const agent = { id: 'worker_1', name: 'desktop', runId: 'run:subagent:worker_1' };
  const items = filterSubagentMessages(agent, [
    { id: 'a', role: 'assistant', subagentId: 'worker_1', text: 'a' },
    { id: 'b', role: 'assistant', agent: 'desktop', agentRole: 'subagent', text: 'b' },
    { id: 'c', role: 'assistant', runId: 'run:subagent:worker_1', agentRole: 'subagent', agent: 'desktop', text: 'c' },
    { id: 'd', role: 'assistant', agent: 'root', text: 'd' },
  ]);
  assert.deepEqual(items.map((item) => item.id).sort(), ['a', 'b', 'c']);
});

test('filter tools by main and subagent scope', () => {
  const tools = [
    { id: 't1', name: 'workspace.list' },
    { id: 't2', name: 'workspace.read_file', subagentId: 's1', agentRole: 'subagent' },
  ];
  assert.equal(filterMainTools(tools).length, 1);
  assert.equal(filterSubagentTools({ id: 's1' }, tools).length, 1);
});

test('extractAgentScope reads nested agent fields', () => {
  const scope = extractAgentScope({
    agent: { role: 'subagent', subagent_id: 's9', name: 'backend' },
    payload: { subagent_id: 'ignored' },
  });
  assert.equal(scope.subagentId, 's9');
  assert.equal(scope.agentRole, 'subagent');
  assert.equal(scope.agentName, 'backend');
});
