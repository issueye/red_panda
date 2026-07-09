export const defaultRunSettings = {
  runtimeMode: 'single_core',
  toolPolicy: 'risk_based',
  permissionMode: 'strict',
  providerProfileId: '',
  spawnSubAgents: false,
  subAgentBackend: 'in_process',
  model: '',
  toolAllowlist: '',
  toolDenylist: '',
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
    require_permission: text.includes('/permission'),
    spawn_subagents: current.spawnSubAgents || text.includes('/subagent'),
    subagent_backend: current.subAgentBackend,
  };
}
