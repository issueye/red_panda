export const defaultRunSettings = {
  runtimeMode: 'single_core',
  toolPolicy: 'risk_based',
  permissionMode: 'strict',
  providerProfileId: '',
  spawnSubAgents: false,
  subAgentBackend: 'process_pool',
  model: '',
  toolAllowlist: '',
  toolDenylist: '',
  webSearchResults: 8,
  webFetchMaxBytes: 2097152,
  maxToolTurns: 4,
};

export function splitOptionList(value) {
  if (!value) {
    return [];
  }
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

export function buildRunStartOptions(settings, workspace, text) {
  const current = { ...defaultRunSettings, ...(settings || {}) };
  return {
    working_dir: workspace?.root_path || workspace?.root || '',
    runtime_mode: current.runtimeMode,
    tool_policy: current.toolPolicy,
    permission_mode: current.permissionMode,
    provider_profile_id: current.providerProfileId,
    model: current.model.trim(),
    tool_allowlist: splitOptionList(current.toolAllowlist),
    tool_denylist: splitOptionList(current.toolDenylist),
    web_search_max_results: Number.isFinite(Number(current.webSearchResults))
      ? Number(current.webSearchResults)
      : 0,
    web_fetch_max_bytes: Number.isFinite(Number(current.webFetchMaxBytes))
      ? Number(current.webFetchMaxBytes)
      : 0,
    max_tool_turns: Number.isFinite(Number(current.maxToolTurns))
      ? Number(current.maxToolTurns)
      : 0,
    require_permission: text.includes('/permission'),
    spawn_subagents: current.spawnSubAgents || text.includes('/subagent'),
    subagent_backend: current.subAgentBackend,
  };
}
