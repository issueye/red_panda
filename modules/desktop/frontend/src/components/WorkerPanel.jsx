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

export function WorkerPanel({ workers = [], assignments = [], onCancelAssignment }) {
  return (
    <section className="worker-panel-content">
      <PanelHeader title="Worker" />
      {workers.length === 0 ? <EmptyState title="Worker 池尚未就绪" /> : (
        <div className="worker-list">
          {workers.map((worker) => {
            const status = worker.state || 'ready';
            const active = status === 'busy' || status === 'draining';
            return (
              <article
                className={classNames('worker-item', active && 'is-running', `status-${status}`)}
                data-status={status}
                data-testid="worker-item"
                key={worker.id}
              >
                <span className={classNames('worker-icon', active && 'is-active')} aria-hidden="true">
                  {active ? <Loader2 className="worker-spin" size={15} /> : <Bot size={16} />}
                </span>
                <div className="worker-main">
                  <span className="worker-heading">
                    <strong>{worker.id}</strong>
                    <StatusBadge status={status} />
                  </span>
                  <span className="worker-meta">{worker.healthy === false ? '执行器异常' : '执行器正常'}</span>
                  {worker.currentAssignmentId ? <small>{worker.currentAssignmentId}</small> : null}
                </div>
              </article>
            );
          })}
        </div>
      )}
      <div className="worker-section-label">工作分配</div>
      {assignments.length === 0 ? <EmptyState title="暂无工作分配" /> : (
        <div className="worker-list">
          {assignments.map((assignment) => {
            const status = assignment.status || 'queued';
            const active = isActiveStatus(status);
            const canCancel = status === 'queued' || status === 'running' || status === 'waiting_permission';
            const title = assignment.profileKey || assignment.workerId || assignment.id;
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
                <div className="worker-main">
                  <span className="worker-heading">
                    <strong>{displayWorkerProfileName(title)}</strong>
                    <StatusBadge status={status} />
                  </span>
                  <span className="worker-meta">
                    {assignment.workerId || '等待 Worker'} · 事件 {formatSeq(assignment.workerSeq)}
                  </span>
                  {assignment.task ? <em>{assignment.task}</em> : null}
                  {assignment.summary || assignment.error ? <small>{assignment.error || assignment.summary}</small> : null}
                </div>
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
    </section>
  );
}
