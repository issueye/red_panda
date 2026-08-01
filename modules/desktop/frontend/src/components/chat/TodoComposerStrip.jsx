import {
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  GripVertical,
  ListChecks,
  LoaderCircle,
  RefreshCw,
  X,
} from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { classNames } from '../../lib/format.js';
import { todoStatusLabel } from '../../lib/todos.js';
import { IconButton } from '../ui/button.jsx';

const TODO_POSITION_STORAGE_KEY = 'red_panda_todo_strip_position_v1';
const TODO_DRAG_MARGIN = 8;

function TodoStatusIcon({ status }) {
  if (status === 'completed') return <Check aria-hidden size={13} strokeWidth={2.5} />;
  if (status === 'in_progress') {
    return <LoaderCircle aria-hidden className="todo-composer-item-spinner" size={13} />;
  }
  if (status === 'cancelled') return <X aria-hidden size={13} />;
  return <Circle aria-hidden size={12} />;
}

function readStoredTodoPosition() {
  if (typeof window === 'undefined') return { x: 0, y: 0 };
  try {
    const value = JSON.parse(window.localStorage.getItem(TODO_POSITION_STORAGE_KEY) || '{}');
    return {
      x: Number.isFinite(value.x) ? value.x : 0,
      y: Number.isFinite(value.y) ? value.y : 0,
    };
  } catch {
    return { x: 0, y: 0 };
  }
}

function storeTodoPosition(position) {
  try {
    window.localStorage.setItem(TODO_POSITION_STORAGE_KEY, JSON.stringify(position));
  } catch {
    // Position persistence is optional in restricted webviews.
  }
}

/**
 * Collapsible task list above the chat composer.
 * Hidden when empty (after hydrate).
 */
export function TodoComposerStrip({
  items = [],
  openCount = 0,
  expanded = false,
  loading = false,
  onToggleExpanded,
  onRefresh,
}) {
  const rootRef = useRef(null);
  const dragRef = useRef(null);
  const positionRef = useRef(readStoredTodoPosition());
  const [position, setPosition] = useState(positionRef.current);
  const [dragging, setDragging] = useState(false);

  const applyPosition = useCallback((next, persist = false) => {
    const normalized = { x: Math.round(next.x), y: Math.round(next.y) };
    positionRef.current = normalized;
    setPosition(normalized);
    if (persist) storeTodoPosition(normalized);
  }, []);

  const constrainCurrentPosition = useCallback((persist = false) => {
    if (expanded) return;
    const root = rootRef.current;
    const bounds = root?.closest('.conversation-view')?.getBoundingClientRect();
    const rect = root?.querySelector('.todo-composer-bar')?.getBoundingClientRect();
    if (!bounds || !rect) return;
    let dx = 0;
    let dy = 0;
    if (rect.left < bounds.left + TODO_DRAG_MARGIN) dx = bounds.left + TODO_DRAG_MARGIN - rect.left;
    if (rect.right > bounds.right - TODO_DRAG_MARGIN) dx = bounds.right - TODO_DRAG_MARGIN - rect.right;
    if (rect.top < bounds.top + TODO_DRAG_MARGIN) dy = bounds.top + TODO_DRAG_MARGIN - rect.top;
    if (rect.bottom > bounds.bottom - TODO_DRAG_MARGIN) dy = bounds.bottom - TODO_DRAG_MARGIN - rect.bottom;
    if (dx || dy) {
      applyPosition({ x: positionRef.current.x + dx, y: positionRef.current.y + dy }, persist);
    }
  }, [applyPosition, expanded]);

  const handleDragPointerDown = useCallback((event) => {
    if (expanded || event.button !== 0) return;
    const root = rootRef.current;
    const bounds = root?.closest('.conversation-view')?.getBoundingClientRect();
    const rect = root?.querySelector('.todo-composer-bar')?.getBoundingClientRect();
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
    const minX = drag.origin.x + drag.bounds.left + TODO_DRAG_MARGIN - drag.rect.left;
    const maxX = drag.origin.x + drag.bounds.right - TODO_DRAG_MARGIN - drag.rect.right;
    const minY = drag.origin.y + drag.bounds.top + TODO_DRAG_MARGIN - drag.rect.top;
    const maxY = drag.origin.y + drag.bounds.bottom - TODO_DRAG_MARGIN - drag.rect.bottom;
    applyPosition({
      x: Math.min(maxX, Math.max(minX, drag.origin.x + event.clientX - drag.startX)),
      y: Math.min(maxY, Math.max(minY, drag.origin.y + event.clientY - drag.startY)),
    });
  }, [applyPosition]);

  const finishDragging = useCallback((event) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    setDragging(false);
    storeTodoPosition(positionRef.current);
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
    const constrain = () => constrainCurrentPosition(true);
    const frame = requestAnimationFrame(constrain);
    window.addEventListener('resize', constrain);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', constrain);
    };
  }, [constrainCurrentPosition]);

  if (!loading && (!items || items.length === 0)) {
    return null;
  }

  const inProgress = items.find((item) => item.status === 'in_progress');
  const pending = items.filter((item) => item.status === 'pending').length;
  const completed = items.filter((item) => item.status === 'completed').length;
  const progress = items.length > 0 ? Math.round((completed / items.length) * 100) : 0;
  const summaryBits = [];
  if (inProgress) summaryBits.push(`进行中 1`);
  if (pending > 0) summaryBits.push(`待办 ${pending}`);
  if (completed > 0) summaryBits.push(`已完成 ${completed}`);
  if (summaryBits.length === 0 && openCount > 0) {
    summaryBits.push(`开放 ${openCount}`);
  }
  if (summaryBits.length === 0) {
    summaryBits.push(`${items.length} 项`);
  }

  const Chevron = expanded ? ChevronDown : ChevronRight;

  return (
    <div
      className={classNames(
        'todo-composer-strip',
        expanded && 'is-expanded',
        dragging && 'is-dragging',
      )}
      data-testid="todo-composer-strip"
      ref={rootRef}
      style={expanded ? undefined : { transform: `translate3d(${position.x}px, ${position.y}px, 0)` }}
    >
      <div className="todo-composer-panel">
        <div className="todo-composer-bar">
          {!expanded ? (
            <button
              aria-label="拖动任务信息"
              className="todo-composer-drag-handle"
              data-testid="todo-drag-handle"
              onDoubleClick={resetPosition}
              onKeyDown={handleDragKeyDown}
              onPointerCancel={finishDragging}
              onPointerDown={handleDragPointerDown}
              onPointerMove={handleDragPointerMove}
              onPointerUp={finishDragging}
              title="拖动调整位置；方向键微调；Home 或双击复位"
              type="button"
            >
              <GripVertical aria-hidden size={13} />
            </button>
          ) : null}
          <button
            aria-expanded={expanded}
            className="todo-composer-toggle"
            data-testid="todo-composer-toggle"
            onClick={onToggleExpanded}
            title="由代理通过 todo 工具维护"
            type="button"
          >
            <span className="todo-composer-icon" aria-hidden="true">
              <ListChecks size={14} />
            </span>
            <span className="todo-composer-copy">
              <span className="todo-composer-heading">
                <span className="todo-composer-title">任务</span>
                {!expanded && inProgress?.content ? (
                  <span className="todo-composer-preview">{inProgress.activeForm || inProgress.content}</span>
                ) : null}
              </span>
              <span className="todo-composer-summary">{summaryBits.join(' · ')}</span>
            </span>
            <span
              aria-hidden="true"
              className="todo-composer-progress"
            >
              <i style={{ width: `${progress}%` }} />
            </span>
            <Chevron aria-hidden className="todo-composer-chevron" size={14} />
          </button>
          {expanded && onRefresh ? (
            <IconButton
              className="todo-composer-refresh"
              label="刷新任务"
              onClick={onRefresh}
              variant="ghost"
            >
              <RefreshCw size={12} />
            </IconButton>
          ) : null}
        </div>
        {expanded ? (
          <ul className="todo-composer-list" data-testid="todo-composer-list">
            {loading && items.length === 0 ? (
              <li className="todo-composer-empty">加载任务…</li>
            ) : (
              items.map((item) => (
                <li
                  className={classNames('todo-composer-item', `is-${item.status}`)}
                  key={item.id || item.clientKey || item.content}
                >
                  <span className="todo-composer-item-icon">
                    <TodoStatusIcon status={item.status} />
                  </span>
                  <div className="todo-composer-item-body">
                    <span className="todo-composer-item-content">{item.content}</span>
                    {item.activeForm && item.status === 'in_progress' ? (
                      <span className="todo-composer-item-active">{item.activeForm}</span>
                    ) : null}
                  </div>
                  <span className="todo-composer-badge">{todoStatusLabel(item.status)}</span>
                </li>
              ))
            )}
          </ul>
        ) : null}
      </div>
    </div>
  );
}
