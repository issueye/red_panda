import assert from 'node:assert/strict';
import test from 'node:test';

import {
  mcpServerCreatePayload,
  mcpServerUpdatePayload,
  normalizeMcpDiscovery,
  normalizeMcpServer,
} from './mcpServers.js';

test('normalizeMcpServer maps API fields and renders one argument per line', () => {
  const server = normalizeMcpServer({
    id: 'mcp_srv_1',
    name: 'filesystem',
    command: 'mcp-filesystem',
    args: ['--root', 'D:/workspace with spaces'],
    env: { TOKEN: '****1234', MODE: 'readonly' },
    cwd: 'D:/workspace',
    enabled: true,
    timeouts: {
      start_ms: 10000,
      initialize_ms: 10000,
      list_ms: 10000,
      call_ms: 30000,
      shutdown_ms: 3000,
    },
    tool_allowlist: ['read_file', 'list'],
    risk_overrides: { read_file: 'low' },
    created_at: '2026-07-09T00:00:00Z',
    updated_at: '2026-07-10T00:00:00Z',
  });

  assert.equal(server.args, '--root\nD:/workspace with spaces');
  assert.deepEqual(server.env, { TOKEN: '****1234', MODE: 'readonly' });
  assert.deepEqual(server.timeouts, {
    start_ms: 10000,
    initialize_ms: 10000,
    list_ms: 10000,
    call_ms: 30000,
    shutdown_ms: 3000,
  });
  assert.deepEqual(server.toolAllowlist, ['read_file', 'list']);
  assert.deepEqual(server.riskOverrides, { read_file: 'low' });
  assert.equal(server.createdAt, '2026-07-09T00:00:00Z');
  assert.equal(server.updatedAt, '2026-07-10T00:00:00Z');
});

test('normalizeMcpDiscovery keeps only read-only discovery fields', () => {
  const result = normalizeMcpDiscovery({
    servers: [{
      name: 'filesystem',
      status: 'connected',
      server_info: { name: 'Filesystem MCP', version: '1.2.0' },
      tools: [{ name: 'read_file', description: 'Read a file', input_schema: { type: 'object' } }],
      duration_ms: 37,
      env: { SECRET: 'must-not-leak' },
    }],
  });

  assert.deepEqual(result, {
    servers: [{
      name: 'filesystem',
      status: 'connected',
      serverInfo: { name: 'Filesystem MCP', version: '1.2.0' },
      tools: [{ name: 'read_file', description: 'Read a file' }],
      error: '',
      stderrSummary: '',
      durationMs: 37,
    }],
  });
  assert.equal(JSON.stringify(result).includes('SECRET'), false);
});

test('normalizeMcpServer keeps masked env values exactly as returned by API', () => {
  const source = { TOKEN: '********', EMPTY: '' };
  const server = normalizeMcpServer({
    id: 'mcp_srv_masked',
    env: source,
  });

  assert.deepEqual(server.env, source);
  assert.notEqual(server.env, source);
  assert.equal(server.env.TOKEN, '********');
});

test('MCP server payloads convert argument lines deterministically', () => {
  const input = {
    name: 'filesystem',
    command: 'mcp-filesystem',
    args: ' --root \r\n D:/workspace with spaces \n\n --readonly ',
    env: { MODE: 'readonly' },
    cwd: 'D:/workspace',
    enabled: false,
    timeouts: { call_ms: 30000 },
    toolAllowlist: ['read_file'],
    riskOverrides: { read_file: 'low' },
  };
  const want = {
    name: 'filesystem',
    command: 'mcp-filesystem',
    args: ['--root', 'D:/workspace with spaces', '--readonly'],
    env: { MODE: 'readonly' },
    cwd: 'D:/workspace',
    enabled: false,
    timeouts: { call_ms: 30000 },
    tool_allowlist: ['read_file'],
    risk_overrides: { read_file: 'low' },
  };

  assert.deepEqual(mcpServerCreatePayload(input), want);
  assert.deepEqual(mcpServerUpdatePayload(input), want);
});

test('MCP server payload defaults keep API-compatible collection shapes', () => {
  assert.deepEqual(mcpServerCreatePayload({
    name: 'minimal',
    command: 'mcp-minimal',
  }), {
    name: 'minimal',
    command: 'mcp-minimal',
    args: [],
    env: {},
    cwd: '',
    enabled: true,
    timeouts: {},
    tool_allowlist: [],
    risk_overrides: {},
  });
});

test('MCP server update payload only includes explicitly provided fields', () => {
  assert.deepEqual(mcpServerUpdatePayload({ enabled: false }), {
    enabled: false,
  });

  const update = mcpServerUpdatePayload({
    name: 'filesystem',
    env: undefined,
    timeouts: undefined,
    toolAllowlist: undefined,
  });
  assert.deepEqual(update, { name: 'filesystem' });
  assert.equal(Object.hasOwn(update, 'env'), false);
  assert.equal(Object.hasOwn(update, 'timeouts'), false);
  assert.equal(Object.hasOwn(update, 'tool_allowlist'), false);
});
