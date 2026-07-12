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
  // When true, Agent Runtime writes each LLM request payload to local diagnostic logs.
  logLlmRequests: false,
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

/**
 * Build run.start options.
 * @param {object} settings
 * @param {object} workspace
 * @param {string} text raw or cleaned composer text (used for /permission /subagent flags)
 * @param {object} [overrides] extra options (create_goal, goal_objective, flags from parseCommand, …)
 */
export function buildRunStartOptions(settings, workspace, text, overrides = {}) {
  const current = { ...defaultRunSettings, ...(settings || {}) };
  const textStr = String(text || '');
  const requirePermission = overrides.require_permission != null
    ? Boolean(overrides.require_permission)
    : textStr.includes('/permission');
  const spawnSubAgents = overrides.spawn_subagents != null
    ? Boolean(overrides.spawn_subagents)
    : (current.spawnSubAgents || textStr.includes('/subagent'));

  const options = {
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
    log_llm_requests: Boolean(current.logLlmRequests),
    require_permission: requirePermission,
    spawn_subagents: spawnSubAgents,
    subagent_backend: current.subAgentBackend,
    // Regular conversations must not enter the Goal pipeline implicitly.
    goals_enabled: false,
  };

  // Goal command overrides (user-initiated create/bind).
  if (overrides.create_goal) {
    options.create_goal = true;
    if (overrides.goal_objective) {
      options.goal_objective = String(overrides.goal_objective);
    }
    if (overrides.goal_title) {
      options.goal_title = String(overrides.goal_title);
    }
    if (overrides.goal_success_criteria) {
      options.goal_success_criteria = String(overrides.goal_success_criteria);
    }
    options.goals_enabled = true;
  }
  if (overrides.goal_id) {
    options.goal_id = String(overrides.goal_id);
  }
  if (overrides.continue_goal) {
    options.continue_goal = true;
  }
  if (overrides.goals_enabled != null) {
    options.goals_enabled = Boolean(overrides.goals_enabled);
  }

  return options;
}
