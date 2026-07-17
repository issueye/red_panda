import { CheckCircle2, ChevronDown, ChevronRight, Circle, CircleX, GripVertical, Play, RefreshCw, Square, Target } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { apiJson } from '../../lib/api.js';
import {
  formatGoalBudget,
  formatGoalActionProgress,
  formatGoalControlBudget,
  goalActionStatusLabel,
  goalCanCancel,
  goalCanContinue,
  goalCriterionStatusLabel,
  goalDisplayTitle,
  goalPauseReasonLabel,
  goalStatusLabel,
  goalVerdictLabel,
} from '../../lib/goals.js';
import {
  goalNoteHeadline,
  goalNoteKindLabel,
  normalizeGoalNoteList,
} from '../../lib/goalNotes.js';
import { classNames } from '../../lib/format.js';
import { Button, IconButton } from '../ui/button.jsx';

const GOAL_POSITION_STORAGE_KEY = 'red_panda_goal_strip_position_v1';
const GOAL_DRAG_MARGIN = 8;

function readStoredGoalPosition() {
  if (typeof window === 'undefined') return { x: 0, y: 0 };
  try {
    const value = JSON.parse(window.localStorage.getItem(GOAL_POSITION_STORAGE_KEY) || '{}');
    return {
      x: Number.isFinite(value.x) ? value.x : 0,
      y: Number.isFinite(value.y) ? value.y : 0,
    };
  } catch {
    return { x: 0, y: 0 };
  }
}

function storeGoalPosition(position) {
  try {
    window.localStorage.setItem(GOAL_POSITION_STORAGE_KEY, JSON.stringify(position));
  } catch {
    // Position persistence is optional in restricted webviews.
  }
}

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
  const rootRef = useRef(null);
  const dragRef = useRef(null);
  const positionRef = useRef(readStoredGoalPosition());
  const [position, setPosition] = useState(positionRef.current);
  const [dragging, setDragging] = useState(false);
  const [notes, setNotes] = useState([]);
  const [notesLoading, setNotesLoading] = useState(false);
  const [notesError, setNotesError] = useState('');
  const [notesHydrated, setNotesHydrated] = useState(false);

  const loadNotes = useCallback(async () => {
    if (!sessionId || !goal?.id) {
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

  const applyPosition = useCallback((next, persist = false) => {
    const normalized = { x: Math.round(next.x), y: Math.round(next.y) };
    positionRef.current = normalized;
    setPosition(normalized);
    if (persist) storeGoalPosition(normalized);
  }, []);

  const constrainCurrentPosition = useCallback((persist = false) => {
    const root = rootRef.current;
    const bounds = root?.closest('.conversation-view')?.getBoundingClientRect();
    const target = expanded
      ? root?.querySelector('.goal-composer-panel')
      : root?.querySelector('.goal-composer-bar');
    const rect = target?.getBoundingClientRect();
    if (!bounds || !rect) return;
    let dx = 0;
    let dy = 0;
    if (rect.left < bounds.left + GOAL_DRAG_MARGIN) dx = bounds.left + GOAL_DRAG_MARGIN - rect.left;
    if (rect.right > bounds.right - GOAL_DRAG_MARGIN) dx = bounds.right - GOAL_DRAG_MARGIN - rect.right;
    if (rect.top < bounds.top + GOAL_DRAG_MARGIN) dy = bounds.top + GOAL_DRAG_MARGIN - rect.top;
    if (rect.bottom > bounds.bottom - GOAL_DRAG_MARGIN) dy = bounds.bottom - GOAL_DRAG_MARGIN - rect.bottom;
    if (dx || dy) {
      applyPosition({ x: positionRef.current.x + dx, y: positionRef.current.y + dy }, persist);
    }
  }, [applyPosition, expanded]);

  const handleDragPointerDown = useCallback((event) => {
    if (event.button !== 0) return;
    const root = rootRef.current;
    const bounds = root?.closest('.conversation-view')?.getBoundingClientRect();
    const target = expanded
      ? root?.querySelector('.goal-composer-panel')
      : root?.querySelector('.goal-composer-bar');
    const rect = target?.getBoundingClientRect();
    if (!bounds || !rect) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      origin: positionRef.current,
      rect,
      bounds,
    };
    setDragging(true);
  }, [expanded]);

  const handleDragPointerMove = useCallback((event) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const deltaX = event.clientX - drag.startX;
    const deltaY = event.clientY - drag.startY;
    const minX = drag.origin.x + drag.bounds.left + GOAL_DRAG_MARGIN - drag.rect.left;
    const maxX = drag.origin.x + drag.bounds.right - GOAL_DRAG_MARGIN - drag.rect.right;
    const minY = drag.origin.y + drag.bounds.top + GOAL_DRAG_MARGIN - drag.rect.top;
    const maxY = drag.origin.y + drag.bounds.bottom - GOAL_DRAG_MARGIN - drag.rect.bottom;
    applyPosition({
      x: Math.min(maxX, Math.max(minX, drag.origin.x + deltaX)),
      y: Math.min(maxY, Math.max(minY, drag.origin.y + deltaY)),
    });
  }, [applyPosition]);

  const finishDragging = useCallback((event) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    setDragging(false);
    storeGoalPosition(positionRef.current);
  }, []);

  const resetPosition = useCallback(() => {
    applyPosition({ x: 0, y: 0 }, true);
  }, [applyPosition]);

  const handleDragKeyDown = useCallback((event) => {
    if (event.key === 'Home') {
      event.preventDefault();
      resetPosition();
      return;
    }
    const amount = event.shiftKey ? 32 : 12;
    const delta = {
      ArrowLeft: [-amount, 0],
      ArrowRight: [amount, 0],
      ArrowUp: [0, -amount],
      ArrowDown: [0, amount],
    }[event.key];
    if (!delta) return;
    event.preventDefault();
    applyPosition({ x: positionRef.current.x + delta[0], y: positionRef.current.y + delta[1] });
    requestAnimationFrame(() => constrainCurrentPosition(true));
  }, [applyPosition, constrainCurrentPosition, resetPosition]);

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

  useEffect(() => {
    const constrain = () => constrainCurrentPosition(true);
    const frame = requestAnimationFrame(constrain);
    window.addEventListener('resize', constrain);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', constrain);
    };
  }, [constrainCurrentPosition, goal?.id]);

  if (!loading && !goal) {
    return null;
  }

  const title = goalDisplayTitle(goal);
  const status = goal?.status || '';
  const budget = formatGoalBudget(goal);
  const actionProgress = formatGoalActionProgress(goal);
  const pauseLabel = goal?.pauseReason ? goalPauseReasonLabel(goal.pauseReason) : '';
  const canContinue = goalCanContinue(goal);
  const canCancelGoal = goalCanCancel(goal);

  const summaryBits = [];
  if (status) summaryBits.push(goalStatusLabel(status));
  if (actionProgress) summaryBits.push(actionProgress);
  if (budget) summaryBits.push(budget);
  if (pauseLabel && status === 'paused') summaryBits.push(pauseLabel);

  const Chevron = expanded ? ChevronDown : ChevronRight;

  return (
    <div
      className={classNames(
        'goal-composer-strip',
        expanded && 'is-expanded',
        dragging && 'is-dragging',
        status && `is-${status}`,
      )}
      data-testid="goal-composer-strip"
      ref={rootRef}
      style={{ transform: `translate3d(${position.x}px, ${position.y}px, 0)` }}
    >
      <div className="goal-composer-panel">
        <div className="goal-composer-bar">
          <button
            aria-label="拖动目标信息"
            className="goal-composer-drag-handle"
            data-testid="goal-drag-handle"
            onDoubleClick={resetPosition}
            onKeyDown={handleDragKeyDown}
            onPointerCancel={finishDragging}
            onPointerDown={handleDragPointerDown}
            onPointerMove={handleDragPointerMove}
            onPointerUp={finishDragging}
            title="拖动目标信息；双击或按 Home 复位"
            type="button"
          >
            <GripVertical aria-hidden size={14} />
          </button>
          <button
            aria-expanded={expanded}
            className="goal-composer-toggle"
            data-testid="goal-composer-toggle"
            onClick={onToggleExpanded}
            title="目标驱动执行：行动、证据与结果评估"
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
              <p className="goal-composer-block goal-contract-objective">
                <strong><Target aria-hidden size={14} /> 期望结果</strong>
                <span>{goal.objective}</span>
              </p>
            ) : null}
            {goal.criteria.length > 0 ? (
              <section className="goal-contract-section" data-testid="goal-criteria">
                <div className="goal-detail-heading">
                  <strong>成功标准</strong>
                  <span>{goal.criteria.filter((item) => item.status === 'met').length}/{goal.criteria.length} 已满足</span>
                </div>
                <ul className="goal-criteria-list">
                  {goal.criteria.map((criterion) => (
                    <li className={classNames('goal-criterion', `is-${criterion.status}`)} key={criterion.id}>
                      {criterion.status === 'met' ? <CheckCircle2 aria-hidden size={14} />
                        : criterion.status === 'not_met' || criterion.status === 'blocked'
                          ? <CircleX aria-hidden size={14} /> : <Circle aria-hidden size={14} />}
                      <div>
                        <strong>{criterion.description}</strong>
                        <span>{goalCriterionStatusLabel(criterion.status)}</span>
                        {criterion.evidence ? <small>{criterion.evidence}</small> : null}
                      </div>
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
            {goal.currentAction ? (
              <p className="goal-composer-block is-current-step" data-testid="goal-current-action">
                <strong>当前行动</strong>
                <span>{goal.currentAction}</span>
              </p>
            ) : null}
            {goal.actions.length > 0 ? (
              <section className="goal-contract-section" data-testid="goal-actions">
                <div className="goal-detail-heading"><strong>行动队列</strong><span>{actionProgress}</span></div>
                <ol className="goal-action-list">
                  {goal.actions.map((action) => (
                    <li className={classNames('goal-action-item', `is-${action.status}`)} key={action.id || action.key}>
                      <span className="goal-action-index">{action.sortOrder + 1}</span>
                      <div>
                        <strong>{action.title}</strong>
                        <span>{goalActionStatusLabel(action.status)}</span>
                        {action.acceptance ? <small>验收：{action.acceptance}</small> : null}
                        {action.result ? <small>结果：{action.result}</small> : null}
                      </div>
                    </li>
                  ))}
                </ol>
              </section>
            ) : null}
            {goal.lastAssessment ? (
              <div className={classNames('goal-evaluation', `is-${goal.lastAssessment.verdict}`)} data-testid="goal-assessment">
                {goal.lastAssessment.verdict === 'satisfied' || goal.lastAssessment.verdict === 'progress'
                  ? <CheckCircle2 aria-hidden size={15} /> : <CircleX aria-hidden size={15} />}
                <div>
                  <strong>最近评估：{goalVerdictLabel(goal.lastAssessment.verdict)}</strong>
                  <span>{goal.lastAssessment.summary}</span>
                  {goal.lastAssessment.gap ? <small>剩余差距：{goal.lastAssessment.gap}</small> : null}
                  {goal.lastAssessment.decision ? <small>下一决策：{goal.lastAssessment.decision}</small> : null}
                </div>
              </div>
            ) : null}
            {goal.strategy ? (
              <p className="goal-composer-block"><strong>当前策略</strong><span>{goal.strategy}</span></p>
            ) : null}
            {formatGoalControlBudget(goal) ? (
              <p className="goal-composer-block" data-testid="goal-control-budget">
                <strong>控制预算</strong><span>{formatGoalControlBudget(goal)}</span>
              </p>
            ) : null}
            {goal.outcomeSummary ? <p className="goal-composer-block"><strong>结果</strong><span>{goal.outcomeSummary}</span></p> : null}
            {goal.reportMarkdown ? (
              <pre className="goal-composer-report">{goal.reportMarkdown}</pre>
            ) : null}

            <div className="goal-notes-panel" data-testid="goal-notes-panel">
              <div className="goal-notes-header">
                <strong>共享笔记</strong>
                <span className="goal-notes-hint">只读 · Goal scratchpad</span>
                <IconButton
                  data-testid="goal-notes-refresh"
                  disabled={notesLoading || !sessionId}
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
