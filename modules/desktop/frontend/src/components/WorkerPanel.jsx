import { Bot, CircleStop, Loader2 } from 'lucide-react';
import { displayWorkerProfileName } from '../lib/displayLabels.js';
import { classNames } from '../lib/format.js';
import { StatusBadge } from './ui/badge.jsx';
import { IconButton } from './ui/button.jsx';
import { EmptyState } from './ui/feedback.jsx';

function isActiveStatus(status) {
  return status === 'queued'
    || status === 'running'
    || status === 'cancelling'
    || status === 'waiting_permission'
    || status === 'paused';
}

function truncate(text, max = 72) {
  const value = String(text || '').trim().replace(/\s+/g, ' ');
  if (!value) return '';
  return value.length > max ? `${value.slice(0, max)}…` : value;
}

function shortWorkerId(value) {
  const text = String(value || '').trim();
  if (!text) return '等待 Worker';
  if (text.length <= 14) return text;
  return `${text.slice(0, 10)}…`;
}

function assignmentDetail(assignment) {
  if (assignment.retrying) {
    return `失败，第 ${Math.max(assignment.attempt || 1, 1) + 1} / 2 次尝试准备中`;
  }
  if (assignment.error) return truncate(assignment.error, 96);
  if (assignment.summary) return truncate(assignment.summary, 96);
  return '';
}

export function WorkerPanel({
  assignments = [],
  onCancelAssignment,
  onOpenAssignment,
}) {
  const activeCount = assignments.filter((item) => isActiveStatus(item.status)).length;

  return (
    <section className="worker-panel-content" data-testid="worker-panel-content">
      <section className="worker-assignment-region" data-testid="worker-assignment-region">
        <div className="worker-section-heading">
          <div className="worker-section-label" data-testid="worker-assignment-section-label">
            工作分配
          </div>
          <span className="worker-section-summary">
            {assignments.length} 项
            {activeCount > 0 ? ` · ${activeCount} 进行中` : ''}
          </span>
        </div>
        <div className="worker-assignment-scroll" data-testid="worker-assignment-scroll">
          {assignments.length === 0 ? (
            <EmptyState title="暂无工作分配">
              主代理调用 Worker 后会出现在这里。
            </EmptyState>
          ) : (
            <div className="worker-list">
              {assignments.map((assignment) => {
                const status = assignment.status || 'queued';
                const active = isActiveStatus(status);
                const failed = status === 'failed' || Boolean(assignment.error);
                const canCancel = status === 'queued'
                  || status === 'running'
                  || status === 'waiting_permission';
                const title = assignment.profileKey || assignment.workerId || assignment.id;
                const task = truncate(assignment.task, 80);
                const detail = assignmentDetail(assignment);
                return (
                  <article
                    className={classNames(
                      'worker-item',
                      active && 'is-running',
                      failed && 'is-failed',
                      `status-${status}`,
                    )}
                    data-status={status}
                    data-testid="worker-assignment"
                    key={assignment.id}
                  >
                    <span className={classNames('worker-icon', active && 'is-active', failed && 'is-failed')} aria-hidden="true">
                      {active ? <Loader2 className="worker-spin" size={15} /> : <Bot size={16} />}
                    </span>
                    <button
                      className="worker-main"
                      data-testid="worker-assignment-open"
                      onClick={() => onOpenAssignment?.(assignment)}
                      type="button"
                    >
                      <span className="worker-heading">
                        <strong title={displayWorkerProfileName(title)}>
                          {displayWorkerProfileName(title)}
                        </strong>
                        <StatusBadge status={status} />
                      </span>
                      {task ? (
                        <em className="worker-task" title={assignment.task}>{task}</em>
                      ) : null}
                      <span className="worker-meta" title={assignment.workerId || ''}>
                        {shortWorkerId(assignment.workerId)}
                        {assignment.attempt > 1 ? ` · 尝试 ${assignment.attempt}/2` : ''}
                      </span>
                      {detail ? (
                        <small
                          className={classNames('worker-detail', failed && 'is-error')}
                          title={assignment.error || assignment.summary || detail}
                        >
                          {detail}
                        </small>
                      ) : null}
                    </button>
                    <div className="worker-actions">
                      <IconButton
                        data-testid="worker-assignment-cancel"
                        disabled={!canCancel}
                        label={canCancel ? '取消工作分配' : '当前工作分配不可取消'}
                        onClick={() => onCancelAssignment?.(assignment)}
                      >
                        <CircleStop size={15} />
                      </IconButton>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </div>
      </section>
    </section>
  );
}
