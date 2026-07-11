/**
 * Helpers for splitting the unified timeline into main vs subagent conversation scopes.
 */

function hasSubagentScope(item) {
  return Boolean(
    item?.subagentId
    || item?.agentRole === 'subagent'
    || (item?.agent && item.agent !== 'root' && item.agent !== 'assistant' && item.agent !== 'gateway' && item.agent !== 'system' && item.role !== 'user'),
  );
}

/**
 * Main conversation: user messages + root/system assistants without subagent ownership.
 * @param {Array} messages
 */
export function filterMainMessages(messages = []) {
  return messages.filter((item) => {
    if (item.role === 'user') return true;
    if (item.subagentId) return false;
    if (item.agentRole === 'subagent') return false;
    return true;
  });
}

/**
 * @param {Array} tools
 */
export function filterMainTools(tools = []) {
  return tools.filter((item) => !item.subagentId && item.agentRole !== 'subagent');
}

/**
 * @param {Array} permissions
 */
export function filterMainPermissions(permissions = []) {
  return permissions.filter((item) => !item.subagentId && item.agentRole !== 'subagent');
}

/**
 * Subagent conversation content.
 * @param {{ id: string, name?: string, runId?: string }} agent
 * @param {Array} messages
 */
export function filterSubagentMessages(agent, messages = []) {
  if (!agent?.id) return [];
  const name = agent.name || '';
  const runId = agent.runId || '';
  return messages.filter((item) => {
    if (item.subagentId && item.subagentId === agent.id) return true;
    if (name && item.agent === name && (item.agentRole === 'subagent' || hasSubagentScope(item))) return true;
    if (runId && item.runId && (item.runId === runId || String(item.runId).includes(agent.id))) return true;
    return false;
  });
}

/**
 * @param {{ id: string, name?: string, runId?: string }} agent
 * @param {Array} tools
 */
export function filterSubagentTools(agent, tools = []) {
  if (!agent?.id) return [];
  const name = agent.name || '';
  const runId = agent.runId || '';
  return tools.filter((item) => {
    if (item.subagentId && item.subagentId === agent.id) return true;
    if (name && item.agent === name && item.agentRole === 'subagent') return true;
    if (runId && (item.runId === runId || item.rootRunId === runId || String(item.runId || '').includes(agent.id))) return true;
    return false;
  });
}

/**
 * @param {{ id: string, name?: string, runId?: string }} agent
 * @param {Array} permissions
 */
export function filterSubagentPermissions(agent, permissions = []) {
  if (!agent?.id) return [];
  const name = agent.name || '';
  return permissions.filter((item) => {
    if (item.subagentId && item.subagentId === agent.id) return true;
    if (name && (item.agent === name || item.agentName === name)) return true;
    return false;
  });
}

/**
 * Extract subagent metadata from a gateway event payload.
 * @param {object} payload
 */
export function extractAgentScope(payload = {}) {
  const agent = payload.agent || {};
  const subagentId = agent.subagent_id || payload.payload?.subagent_id || '';
  const agentRole = agent.role || payload.agent_role || (subagentId ? 'subagent' : 'root');
  const agentName = payload.payload?.name || agent.name || agent.role || payload.agent_name || payload.agent_role || 'agent';
  return {
    subagentId: subagentId || '',
    agentRole: agentRole || '',
    agentName: agentName || '',
  };
}
