/**
 * Managed Worker Profile definitions (Gateway /api/v1/worker-profiles).
 * Field `phase` on the wire is a capability tag only (docs/41 W3-4) — not a
 * Goal pipeline stage.
 */

import { listFromEnvelope } from './envelope.js';

/** Capability tags for Worker Profiles (API field remains `phase`). */
export const AGENT_CAPABILITY_LABELS = {
  // V2 capability tags (builtin seeds).
  research: '调研',
  strategy: '策略',
  build: '实施',
  review: '验证',
  assess: '评估',
  // Legacy pipeline-era labels (still accepted).
  analyze: '分析',
  plan: '规划',
  execute: '执行',
  verify: '验证',
  evaluate: '终评',
  general: '通用',
  custom: '自定义',
};

/** @deprecated use AGENT_CAPABILITY_LABELS */
export const AGENT_PHASE_LABELS = AGENT_CAPABILITY_LABELS;

export function agentCapabilityLabel(capability) {
  return AGENT_CAPABILITY_LABELS[capability] || capability || '自定义';
}

/** @deprecated use agentCapabilityLabel */
export function agentPhaseLabel(phase) {
  return agentCapabilityLabel(phase);
}

export function emptyAgentDraft() {
  return {
    id: '',
    key: '',
    name: '',
    name_zh: '',
    phase: 'custom', // capability tag (wire name)
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
  return listFromEnvelope(payload, { keys: ['items', 'data'] });
}
