import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
  formatGoalActionProgress,
  formatGoalBudget,
  formatGoalControlBudget,
  goalCanCancel,
  goalCanContinue,
  goalDisplayTitle,
  goalFromUpdatedEvent,
  goalShouldAutoContinue,
  goalShouldResumeAfterCompact,
  normalizeGoal,
  normalizeGoalList,
  pickFocusGoal,
} from './goals.js';

describe('Goal V2 helpers', () => {
  it('normalizes the outcome contract and controller projection', () => {
    const goal = normalizeGoal({
      id: 'g1', status: 'active', objective: 'Ship behavior',
      criteria: [{ id: 'c1', description: 'Works', status: 'met', evidence: 'test' }],
      constraints: ['No API break'], strategy: 'Close the riskiest gap first',
      current_action_id: 'a1', current_action: 'Run integration test',
      actions: [{ id: 'a1', key: 'test', title: 'Run integration test', status: 'active', sort_order: 0 }],
      last_assessment: { verdict: 'progress', summary: 'Implementation works', gap: 'Run full suite' },
      iteration: 2, max_iterations: 10, stagnation_count: 0, max_stagnation: 3,
    });
    assert.equal(goal.criteria[0].status, 'met');
    assert.equal(goal.actions[0].title, 'Run integration test');
    assert.equal(goal.lastAssessment.verdict, 'progress');
    assert.equal(goal.currentActionId, 'a1');
  });

  it('picks active then paused goal', () => {
    const items = normalizeGoalList({ items: [
      { id: 'done', status: 'succeeded' }, { id: 'paused', status: 'paused' }, { id: 'active', status: 'active' },
    ] });
    assert.equal(pickFocusGoal(items).id, 'active');
    assert.equal(pickFocusGoal(items.filter((item) => item.status !== 'active')).id, 'paused');
  });

  it('auto-continues only an evidence-backed progress assessment', () => {
    const base = {
      status: 'paused', pause_reason: 'awaiting_continue', iteration: 2, max_iterations: 10,
      stagnation_count: 0, max_stagnation: 3, used_tool_turns: 20, max_total_tool_turns: 96,
    };
    const progress = normalizeGoal({ ...base, last_assessment: { verdict: 'progress', summary: 'gap reduced' } });
    assert.equal(goalShouldAutoContinue(progress), true);
    assert.equal(goalCanContinue(progress), false);
    assert.equal(goalShouldAutoContinue(normalizeGoal(base)), false);
    assert.equal(goalCanContinue(normalizeGoal(base)), true);
    assert.equal(goalShouldAutoContinue(normalizeGoal({ ...base, last_assessment: { verdict: 'blocked' } })), false);
    assert.equal(goalCanCancel(normalizeGoal({ status: 'active' })), true);
    assert.equal(goalCanCancel(normalizeGoal({ status: 'succeeded' })), false);
  });

  it('resumes a Goal paused specifically for context compact', () => {
    assert.equal(goalShouldResumeAfterCompact(normalizeGoal({ status: 'paused', pause_reason: 'session_compact' })), true);
    assert.equal(goalShouldResumeAfterCompact(normalizeGoal({ status: 'paused', pause_reason: 'user_cancel' })), false);
    assert.equal(goalShouldResumeAfterCompact(normalizeGoal({ status: 'active', pause_reason: 'session_compact' })), false);
  });

  it('formats action and controller budgets without transport segments', () => {
    const goal = normalizeGoal({
      actions: [{ status: 'done' }, { status: 'active' }],
      iteration: 2, max_iterations: 10, stagnation_count: 1, max_stagnation: 3,
      used_tool_turns: 7, max_total_tool_turns: 96, used_wall_time_sec: 30, max_wall_time_sec: 1800,
    });
    assert.equal(formatGoalActionProgress(goal), '行动 1/2');
    assert.equal(formatGoalBudget(goal), '迭代 2/10 · 工具 7/96');
    assert.equal(formatGoalControlBudget(goal), '迭代 2/10 · 工具 7/96 · 停滞 1/3 · 耗时 30/1800s');
  });

  it('parses goal_updated payload and display title', () => {
    const goal = goalFromUpdatedEvent({ action: 'assess', goal: { id: 'g1', status: 'active', objective: 'Deliver result' } });
    assert.equal(goal.id, 'g1');
    assert.equal(goalDisplayTitle(goal), 'Deliver result');
  });
});
