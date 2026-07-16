import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
  agentCapabilityLabel,
  agentDisplayName,
  agentDraftFrom,
  agentPhaseLabel,
  emptyAgentDraft,
  normalizeAgentList,
} from './agents.js';

describe('agents helpers', () => {
  it('labels known capability tags', () => {
    assert.equal(agentCapabilityLabel('research'), '调研');
    assert.equal(agentCapabilityLabel('build'), '实施');
    assert.equal(agentPhaseLabel('analyze'), '分析'); // legacy alias
    assert.equal(agentPhaseLabel('verify'), '验证');
  });

  it('normalizes list envelopes', () => {
    assert.equal(normalizeAgentList([{ id: '1' }]).length, 1);
    assert.equal(normalizeAgentList({ items: [{ id: '1' }, { id: '2' }] }).length, 2);
  });

  it('draft round-trip basics', () => {
    const draft = agentDraftFrom({
      id: 'a1',
      key: 'goal-analyst',
      name: 'Goal Analyst',
      name_zh: '目标分析师',
      phase: 'analyze',
      default_max_turns: 12,
      enabled: true,
      builtin: true,
    });
    assert.equal(draft.builtin, true);
    assert.equal(draft.isNew, false);
    assert.equal(agentDisplayName(draft), '目标分析师');
    assert.equal(emptyAgentDraft().isNew, true);
  });
});
