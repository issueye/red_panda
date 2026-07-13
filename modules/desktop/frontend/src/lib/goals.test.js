import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
  formatGoalBudget,
  formatGoalBudgetAdvanced,
  goalCanCancel,
  goalCanContinue,
  goalShouldAutoContinue,
  goalDisplayTitle,
  goalFromUpdatedEvent,
  normalizeGoal,
  normalizeGoalList,
  pickFocusGoal,
} from './goals.js';

describe('goals helpers', () => {
  it('normalizes and picks focus goal', () => {
    const items = normalizeGoalList({
      items: [
        { id: 'g1', status: 'succeeded', objective: 'done' },
        { id: 'g2', status: 'paused', title: '续跑', pause_reason: 'awaiting_continue' },
        { id: 'g3', status: 'active', objective: 'working' },
      ],
    });
    assert.equal(pickFocusGoal(items).id, 'g3');
    assert.equal(pickFocusGoal(items.filter((g) => g.status !== 'active')).id, 'g2');
  });

  it('continue/cancel gates', () => {
    assert.equal(goalCanContinue(normalizeGoal({ status: 'paused' })), true);
    const auto = normalizeGoal({
      status: 'paused',
      pause_reason: 'awaiting_continue',
      used_tool_turns: 71,
      max_total_tool_turns: 96,
    });
    assert.equal(goalShouldAutoContinue(auto), true);
    assert.equal(goalCanContinue(auto), false);
    assert.equal(goalCanContinue(normalizeGoal({ status: 'active' })), false);
    assert.equal(goalCanCancel(normalizeGoal({ status: 'active' })), true);
    assert.equal(goalCanCancel(normalizeGoal({ status: 'succeeded' })), false);
  });

  it('parses goal_updated payload', () => {
    const goal = goalFromUpdatedEvent({
      action: 'checkpoint',
      goal: { id: 'g1', status: 'active', objective: 'x', used_tool_turns: 3, max_total_tool_turns: 96 },
    });
    assert.equal(goal.id, 'g1');
    assert.equal(formatGoalBudget(goal), '3/96 轮');
    assert.equal(goalDisplayTitle(goal), 'x');
  });

  it('keeps advanced budget details out of the compact bar', () => {
    const goal = normalizeGoal({
      used_tool_turns: 4,
      max_total_tool_turns: 48,
      used_segments: 2,
      max_segments_per_run: 3,
      max_tool_turns_per_segment: 12,
    });
    assert.equal(formatGoalBudget(goal), '4/48 轮');
    assert.equal(formatGoalBudgetAdvanced(goal), '段 2/3 · 每段≤12 轮');
  });
});
