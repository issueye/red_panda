import { ChevronDown, ChevronRight, Play, RefreshCw, Square } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { apiJson } from '../../lib/api.js';
import {
  formatGoalBudget,
  formatGoalBudgetAdvanced,
  goalCanCancel,
  goalCanContinue,
  goalDisplayTitle,
  goalPauseReasonLabel,
  goalPhaseLabel,
  goalStatusLabel,
} from '../../lib/goals.js';
import {
  goalNoteHeadline,
  goalNoteKindLabel,
  normalizeGoalNoteList,
} from '../../lib/goalNotes.js';
import { classNames } from '../../lib/format.js';
import { Button, IconButton } from '../ui/button.jsx';

/**
 * Collapsible goal strip above the todo list / chat composer.
 * Expanded view includes a read-only scratchpad (context notes) projection (docs/36 C4).
 */
export function GoalComposerStrip({
  goal = null,
  sessionId = '',
  expanded = false,
  loading = false,
  busy = false,
  onToggleExpanded,
  onContinue,
  onCancel,
}) {
  const [notes, setNotes] = useState([]);
  const [notesLoading, setNotesLoading] = useState(false);
  const [notesError, setNotesError] = useState('');
  const [notesHydrated, setNotesHydrated] = useState(false);

  const loadNotes = useCallback(async () => {
    if (!sessionId || !goal?.id || sessionId === 'local-design') {
      setNotes([]);
      setNotesError('');
      setNotesHydrated(true);
      return;
    }
    setNotesLoading(true);
    setNotesError('');
    try {
      const data = await apiJson(
        `/api/v1/sessions/${encodeURIComponent(sessionId)}/goals/${encodeURIComponent(goal.id)}/notes`,
      );
      setNotes(normalizeGoalNoteList(data));
      setNotesHydrated(true);
    } catch (error) {
      setNotes([]);
      setNotesError(error?.message || '加载笔记失败');
      setNotesHydrated(true);
    } finally {
      setNotesLoading(false);
    }
  }, [goal?.id, sessionId]);

  useEffect(() => {
    if (!expanded || !goal?.id) return undefined;
    let cancelled = false;
    (async () => {
      if (cancelled) return;
      await loadNotes();
    })();
    return () => {
      cancelled = true;
    };
  }, [expanded, goal?.id, goal?.updatedAt, loadNotes]);

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
            {formatGoalBudgetAdvanced(goal) ? (
              <p className="goal-composer-block" data-testid="goal-budget-advanced">
                <strong>高级预算</strong>
                <span>{formatGoalBudgetAdvanced(goal)}</span>
              </p>
            ) : null}
            {goal.reportMarkdown ? (
              <pre className="goal-composer-report">{goal.reportMarkdown}</pre>
            ) : null}

            <div className="goal-notes-panel" data-testid="goal-notes-panel">
              <div className="goal-notes-header">
                <strong>共享笔记</strong>
                <span className="goal-notes-hint">只读 · Goal scratchpad</span>
                <IconButton
                  data-testid="goal-notes-refresh"
                  disabled={notesLoading || !sessionId || sessionId === 'local-design'}
                  label="刷新笔记"
                  onClick={() => { loadNotes().catch(() => {}); }}
                  variant="ghost"
                >
                  <RefreshCw size={12} className={notesLoading ? 'tool-spin' : undefined} />
                </IconButton>
              </div>
              {notesLoading && !notesHydrated ? (
                <p className="goal-notes-empty">加载笔记…</p>
              ) : null}
              {notesError ? (
                <p className="goal-notes-empty" data-testid="goal-notes-error">{notesError}</p>
              ) : null}
              {!notesLoading && !notesError && notes.length === 0 && notesHydrated ? (
                <p className="goal-notes-empty" data-testid="goal-notes-empty">暂无笔记（模型可通过 context.write 写入）</p>
              ) : null}
              {notes.length > 0 ? (
                <ul className="goal-notes-list" data-testid="goal-notes-list">
                  {notes.map((note) => (
                    <li
                      className={classNames('goal-note-item', note.pinned && 'is-pinned')}
                      data-testid="goal-note-item"
                      key={note.id || `${note.seq}-${note.title}`}
                    >
                      <div className="goal-note-meta">
                        <span className="goal-note-kind">{goalNoteKindLabel(note.kind)}</span>
                        {note.pinned ? <em className="goal-note-pin">置顶</em> : null}
                        {note.phase ? <span className="goal-note-phase">{note.phase}</span> : null}
                        {note.source ? <span className="goal-note-source">{note.source}</span> : null}
                      </div>
                      <strong className="goal-note-title">{goalNoteHeadline(note)}</strong>
                      {note.body ? (
                        <p className="goal-note-body">{note.body}</p>
                      ) : null}
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
