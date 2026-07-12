import { ChevronDown, ChevronRight, Play, Square } from 'lucide-react';
import {
  formatGoalBudget,
  goalCanCancel,
  goalCanContinue,
  goalDisplayTitle,
  goalPauseReasonLabel,
  goalPhaseLabel,
  goalStatusLabel,
} from '../../lib/goals.js';
import { classNames } from '../../lib/format.js';
import { Button, IconButton } from '../ui/button.jsx';

/**
 * Collapsible goal strip above the todo list / chat composer.
 */
export function GoalComposerStrip({
  goal = null,
  expanded = false,
  loading = false,
  busy = false,
  onToggleExpanded,
  onContinue,
  onCancel,
}) {
  if (!loading && !goal) {
    return null;
  }

  const title = goalDisplayTitle(goal);
  const status = goal?.status || '';
  const phase = goal?.pipelinePhase || '';
  const budget = formatGoalBudget(goal);
  const pauseLabel = goal?.pauseReason ? goalPauseReasonLabel(goal.pauseReason) : '';
  const canContinue = goalCanContinue(goal);
  const canCancelGoal = goalCanCancel(goal);

  const summaryBits = [];
  if (status) summaryBits.push(goalStatusLabel(status));
  if (phase) summaryBits.push(goalPhaseLabel(phase));
  if (budget) summaryBits.push(budget);
  if (pauseLabel && status === 'paused') summaryBits.push(pauseLabel);

  const Chevron = expanded ? ChevronDown : ChevronRight;

  return (
    <div
      className={classNames('goal-composer-strip', expanded && 'is-expanded', status && `is-${status}`)}
      data-testid="goal-composer-strip"
    >
      <div className="goal-composer-panel">
        <div className="goal-composer-bar">
          <button
            aria-expanded={expanded}
            className="goal-composer-toggle"
            data-testid="goal-composer-toggle"
            onClick={onToggleExpanded}
            title="长程目标（分析 → 步骤 → 执行验证 → 报告）"
            type="button"
          >
            <Chevron aria-hidden className="goal-composer-chevron" size={14} />
            <span className="goal-composer-title">目标</span>
            <span className="goal-composer-name">{title || (loading ? '加载中…' : '')}</span>
            {summaryBits.length > 0 ? (
              <span className="goal-composer-summary">{summaryBits.join(' · ')}</span>
            ) : null}
          </button>
          <div className="goal-composer-actions">
            {canContinue ? (
              <Button
                data-testid="goal-continue"
                disabled={busy}
                icon={<Play size={12} />}
                onClick={onContinue}
                variant="soft"
              >
                继续
              </Button>
            ) : null}
            {canCancelGoal ? (
              <IconButton
                data-testid="goal-cancel"
                disabled={busy}
                label="取消目标"
                onClick={onCancel}
                variant="ghost"
              >
                <Square size={12} />
              </IconButton>
            ) : null}
          </div>
        </div>
        {expanded && goal ? (
          <div className="goal-composer-detail" data-testid="goal-composer-detail">
            {goal.objective ? (
              <p className="goal-composer-block">
                <strong>目标</strong>
                <span>{goal.objective}</span>
              </p>
            ) : null}
            {goal.successCriteria ? (
              <p className="goal-composer-block">
                <strong>成功标准</strong>
                <span>{goal.successCriteria}</span>
              </p>
            ) : null}
            {goal.checkpointSummary ? (
              <p className="goal-composer-block">
                <strong>检查点</strong>
                <span>{goal.checkpointSummary}</span>
              </p>
            ) : null}
            {goal.progressNote ? (
              <p className="goal-composer-block">
                <strong>进度</strong>
                <span>{goal.progressNote}</span>
              </p>
            ) : null}
            {goal.reportMarkdown ? (
              <pre className="goal-composer-report">{goal.reportMarkdown}</pre>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
