import { mapListFromEnvelope } from './envelope.js';

export function normalizeSkillSummary(item = {}) {
  return {
    name: item.name || '',
    description: item.description || '',
    path: item.path || '',
    hasInstructions: Boolean(item.has_instructions),
  };
}

export function normalizeSkillDetail(item = {}) {
  const skill = item.skill || item;
  return {
    name: skill.name || '',
    description: skill.description || '',
    instructions: skill.instructions || '',
    path: skill.path || '',
    sizeBytes: Number.isFinite(skill.size_bytes) ? skill.size_bytes : 0,
  };
}

export function skillCreatePayload(input = {}, workspaceRoot = '') {
  return {
    workspace_root: workspaceRoot || input.workspaceRoot || '',
    name: String(input.name || '').trim(),
    description: String(input.description || '').trim(),
    instructions: String(input.instructions || '').trim(),
  };
}

export function skillUpdatePayload(input = {}, workspaceRoot = '') {
  return {
    workspace_root: workspaceRoot || input.workspaceRoot || '',
    description: String(input.description || '').trim(),
    instructions: String(input.instructions || '').trim(),
  };
}

export function normalizeSkillsList(data = {}) {
  return mapListFromEnvelope(data, normalizeSkillSummary);
}
