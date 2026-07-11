export const defaultRunSettings = {
  // Isolate concurrent multi-session runs in dedicated agent processes by default.
  runtimeMode: 'per_run_process',
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
  webSearchProvider: 'auto',
  webTavilyApiKey: '',
  webHttpProxy: '',
  maxToolTurns: 12,
  maxConcurrentRuns: 3,
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
    web_search_provider: String(current.webSearchProvider || 'auto').trim() || 'auto',
    web_tavily_api_key: String(current.webTavilyApiKey || '').trim(),
    web_http_proxy: String(current.webHttpProxy || '').trim(),
    max_tool_turns: Number.isFinite(Number(current.maxToolTurns))
      ? Number(current.maxToolTurns)
      : 0,
    max_concurrent_runs: Number.isFinite(Number(current.maxConcurrentRuns))
      ? Number(current.maxConcurrentRuns)
      : 0,
    require_permission: text.includes('/permission'),
    spawn_subagents: current.spawnSubAgents || text.includes('/subagent'),
    subagent_backend: current.subAgentBackend,
  };
}
