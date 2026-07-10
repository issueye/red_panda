export function normalizeMcpServer(item = {}) {
  return {
    id: item.id,
    name: item.name || item.id || '',
    command: item.command || '',
    args: argsText(item.args),
    env: objectValue(item.env),
    cwd: item.cwd || '',
    enabled: item.enabled !== false,
    timeouts: objectValue(item.timeouts),
    toolAllowlist: arrayValue(item.tool_allowlist),
    riskOverrides: objectValue(item.risk_overrides),
    createdAt: item.created_at,
    updatedAt: item.updated_at,
  };
}

export function mcpServerCreatePayload(input = {}) {
  return mcpServerPayload(input);
}

export function mcpServerUpdatePayload(input = {}) {
  const payload = {};
  assignProvided(payload, input, 'name', 'name');
  assignProvided(payload, input, 'command', 'command');
  assignProvided(payload, input, 'args', 'args', argsArray);
  assignProvided(payload, input, 'env', 'env', objectValue);
  assignProvided(payload, input, 'cwd', 'cwd');
  assignProvided(payload, input, 'enabled', 'enabled', Boolean);
  assignProvided(payload, input, 'timeouts', 'timeouts', objectValue);
  assignProvided(payload, input, 'toolAllowlist', 'tool_allowlist', arrayValue);
  assignProvided(payload, input, 'riskOverrides', 'risk_overrides', objectValue);
  return payload;
}

export function normalizeMcpDiscovery(data = {}) {
  return {
    servers: Array.isArray(data.servers) ? data.servers.map((server) => ({
      name: server.name || '',
      status: server.status || 'unknown',
      serverInfo: {
        name: server.server_info?.name || '',
        version: server.server_info?.version || '',
      },
      tools: Array.isArray(server.tools) ? server.tools.map((tool) => ({
        name: tool.name || '',
        description: tool.description || '',
      })) : [],
      error: server.error || '',
      stderrSummary: server.stderr_summary || '',
      durationMs: Number.isFinite(server.duration_ms) ? server.duration_ms : 0,
    })) : [],
  };
}

function mcpServerPayload(input) {
  return {
    name: input.name || '',
    command: input.command || '',
    args: argsArray(input.args),
    env: objectValue(input.env),
    cwd: input.cwd || '',
    enabled: input.enabled !== false,
    timeouts: objectValue(input.timeouts),
    tool_allowlist: arrayValue(input.toolAllowlist),
    risk_overrides: objectValue(input.riskOverrides),
  };
}

function argsText(value) {
  if (!Array.isArray(value)) {
    return '';
  }
  return value.map((item) => String(item)).join('\n');
}

function argsArray(value) {
  if (Array.isArray(value)) {
    return value.map((item) => String(item));
  }
  if (typeof value !== 'string' || !value.trim()) {
    return [];
  }
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function arrayValue(value) {
  return Array.isArray(value) ? [...value] : [];
}

function objectValue(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {};
}

function assignProvided(target, source, sourceKey, targetKey, transform = (value) => value) {
  if (Object.hasOwn(source, sourceKey) && source[sourceKey] !== undefined) {
    target[targetKey] = transform(source[sourceKey]);
  }
}
