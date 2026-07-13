/**
 * Managed Worker Profile definitions (Gateway /api/v1/worker-profiles).
 */

export const AGENT_PHASE_LABELS = {
  analyze: '分析',
  plan: '规划',
  execute: '执行',
  verify: '验证',
  evaluate: '终评',
  general: '通用',
  custom: '自定义',
};

export function agentPhaseLabel(phase) {
  return AGENT_PHASE_LABELS[phase] || phase || '自定义';
}

export function emptyAgentDraft() {
  return {
    id: '',
    key: '',
    name: '',
    name_zh: '',
    phase: 'custom',
    description: '',
    system_prompt: '',
    default_max_turns: 12,
    enabled: true,
    builtin: false,
    isNew: true,
  };
}

export function agentDraftFrom(item) {
  if (!item) return emptyAgentDraft();
  return {
    id: item.id || '',
    key: item.key || '',
    name: item.name || '',
    name_zh: item.name_zh || '',
    phase: item.phase || 'custom',
    description: item.description || '',
    system_prompt: item.system_prompt || '',
    default_max_turns: Number(item.default_max_turns) || 12,
    enabled: item.enabled !== false,
    builtin: Boolean(item.builtin),
    isNew: false,
  };
}

export function agentDisplayName(item) {
  if (!item) return '';
  return item.name_zh || item.name || item.key || item.id || '';
}

/**
 * @param {unknown} payload
 * @returns {Array<object>}
 */
export function normalizeAgentList(payload) {
  if (Array.isArray(payload)) return payload;
  if (payload && Array.isArray(payload.items)) return payload.items;
  if (payload && Array.isArray(payload.data)) return payload.data;
  return [];
}
