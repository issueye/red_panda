export const defaultRunSettings = {
  // Isolate concurrent multi-session runs in dedicated agent processes by default.
  runtimeMode: 'per_run_process',
  toolPolicy: 'risk_based',
  permissionMode: 'strict',
  providerProfileId: '',
  model: '',
  reasoningEffort: '',
  toolAllowlist: '',
  toolDenylist: '',
  webSearchResults: 8,
  webFetchMaxBytes: 2097152,
  webSearchProvider: 'auto',
  webTavilyApiKey: '',
  webHttpProxy: '',
  maxToolTurns: 12,
  maxConcurrentRuns: 3,
  // Worker pool size controls how many reusable delegated specialists (via worker.delegate) can run concurrently.
  // Valid range: 1-8. Default 8. Larger values allow more parallel analysis but use more memory/CPU.
  workerPoolSize: 8,
  // When true, Agent Runtime writes each LLM request payload to local diagnostic logs.
  logLlmRequests: false,
  // When true, plain Enter sends the composer message; Shift+Enter inserts a newline.
  enterToSend: true,
};

export function normalizeStoredRunSettings(settings) {
  const current = { ...defaultRunSettings, ...(settings || {}) };
  const modelOverride = String(current.model || '').trim();
  if (current.providerProfileId && /^\d+$/.test(modelOverride)) {
    current.model = '';
  }
  // Upgrade users who had the old default (2) to the new default (8).
  // Only upgrade exact old default; explicit user values (1,3,4,...) are respected.
  if (current.workerPoolSize === 2) {
    current.workerPoolSize = defaultRunSettings.workerPoolSize;
  }
  // Clamp worker pool size to valid range (0 = use default, 1-8 otherwise)
  current.workerPoolSize = clampWorkerPoolSize(current.workerPoolSize);
  current.enterToSend = current.enterToSend !== false;
  return current;
}

function clampWorkerPoolSize(raw) {
  const n = Number(raw);
  if (!Number.isFinite(n) || n <= 0) {
    return 0; // 0 means "use runtime default (2)"
  }
  if (n < 1) return 1;
  if (n > 8) return 8;
  return Math.floor(n);
}

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
 * @param {string} text raw or cleaned composer text (used for /permission flag)
 * @param {object} [overrides] extra options (flags from parseCommand, …)
 */
export function buildRunStartOptions(settings, workspace, text, overrides = {}) {
  const current = normalizeStoredRunSettings(settings);
  const textStr = String(text || '');
  const requirePermission = overrides.require_permission != null
    ? Boolean(overrides.require_permission)
    : textStr.includes('/permission');

  const options = {
    working_dir: workspace?.root_path || workspace?.root || '',
    runtime_mode: current.runtimeMode,
    tool_policy: current.toolPolicy,
    permission_mode: current.permissionMode,
    provider_profile_id: current.providerProfileId,
    model: String(current.model || '').trim(),
    reasoning_effort: String(current.reasoningEffort || '').trim(),
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
    worker_pool_size: clampWorkerPoolSize(current.workerPoolSize),
    log_llm_requests: Boolean(current.logLlmRequests),
    require_permission: requirePermission,
  };

  return options;
}
