import { ChevronDown, ChevronRight, RefreshCw } from 'lucide-react';
import { classNames } from '../../lib/format.js';
import { todoStatusLabel } from '../../lib/todos.js';
import { IconButton } from '../ui/button.jsx';

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
  if (!loading && (!items || items.length === 0)) {
    return null;
  }

  const inProgress = items.find((item) => item.status === 'in_progress');
  const pending = items.filter((item) => item.status === 'pending').length;
  const completed = items.filter((item) => item.status === 'completed').length;
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
      className={classNames('todo-composer-strip', expanded && 'is-expanded')}
      data-testid="todo-composer-strip"
    >
      <div className="todo-composer-panel">
        <div className="todo-composer-bar">
          <button
            aria-expanded={expanded}
            className="todo-composer-toggle"
            data-testid="todo-composer-toggle"
            onClick={onToggleExpanded}
            title="由代理通过 todo 工具维护"
            type="button"
          >
            <Chevron aria-hidden className="todo-composer-chevron" size={14} />
            <span className="todo-composer-title">任务</span>
            <span className="todo-composer-summary">{summaryBits.join(' · ')}</span>
            {!expanded && inProgress?.content ? (
              <span className="todo-composer-preview">{inProgress.activeForm || inProgress.content}</span>
            ) : null}
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
                  <span className="todo-composer-badge">{todoStatusLabel(item.status)}</span>
                  <div className="todo-composer-item-body">
                    <span className="todo-composer-item-content">{item.content}</span>
                    {item.activeForm && item.status === 'in_progress' ? (
                      <span className="todo-composer-item-active">{item.activeForm}</span>
                    ) : null}
                  </div>
                </li>
              ))
            )}
          </ul>
        ) : null}
      </div>
    </div>
  );
}
