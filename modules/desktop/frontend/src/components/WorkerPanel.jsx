import { Bot, CircleStop, Loader2 } from 'lucide-react';
import { displayWorkerProfileName } from '../lib/displayLabels.js';
import { classNames, formatSeq } from '../lib/format.js';
import { StatusBadge } from './ui/badge.jsx';
import { IconButton } from './ui/button.jsx';
import { EmptyState } from './ui/feedback.jsx';
import { PanelHeader } from './ui/panel.jsx';

function isActiveStatus(status) {
  return status === 'queued' || status === 'running' || status === 'cancelling' || status === 'waiting_permission';
}

export function WorkerPanel({
  assignments = [], onCancelAssignment, onOpenAssignment,
}) {
  return (
    <section className="worker-panel-content" data-testid="worker-panel-content">
      <section className="worker-assignment-region" data-testid="worker-assignment-region">
        <div className="worker-section-heading">
          <div className="worker-section-label" data-testid="worker-assignment-section-label">工作分配</div>
          <span className="worker-section-summary">{assignments.length} 项</span>
        </div>
        <div className="worker-assignment-scroll" data-testid="worker-assignment-scroll">
          {assignments.length === 0 ? <EmptyState title="暂无工作分配" /> : (
            <div className="worker-list">
              {assignments.map((assignment) => {
                const status = assignment.status || 'queued';
                const active = isActiveStatus(status);
                const canCancel = status === 'queued' || status === 'running' || status === 'waiting_permission';
                const title = assignment.profileKey || assignment.workerId || assignment.id;
                const detail = assignment.retrying
                  ? `失败，${Math.max(assignment.attempt || 1, 1) + 1} / 2 次尝试准备中`
                  : (assignment.error || assignment.summary);
                return (
                  <article
                    className={classNames('worker-item', active && 'is-running', `status-${status}`)}
                    data-status={status}
                    data-testid="worker-assignment"
                    key={assignment.id}
                  >
                    <span className={classNames('worker-icon', active && 'is-active')} aria-hidden="true">
                      {active ? <Loader2 className="worker-spin" size={15} /> : <Bot size={16} />}
                    </span>
                    <button
                      className="worker-main"
                      data-testid="worker-assignment-open"
                      onClick={() => onOpenAssignment?.(assignment)}
                      type="button"
                    >
                      <span className="worker-heading">
                        <strong>{displayWorkerProfileName(title)}</strong>
                        <StatusBadge status={status} />
                      </span>
                      <span className="worker-meta">
                        {assignment.workerId || '等待 Worker'} · 事件 {formatSeq(assignment.workerSeq)}
                      </span>
                      {assignment.task ? <em>{assignment.task}</em> : null}
                      {detail ? <small>{detail}</small> : null}
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
