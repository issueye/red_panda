import { useState } from 'react';
import { Bot, ChevronDown, ChevronRight, CircleStop, ExternalLink, Loader2 } from 'lucide-react';
import { displayWorkerProfileName } from '../lib/displayLabels.js';
import { classNames } from '../lib/format.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button, IconButton } from './ui/button.jsx';
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

function shortId(value, max = 14) {
  const text = String(value || '').trim();
  if (!text) return '';
  if (text.length <= max) return text;
  return `${text.slice(0, Math.max(6, max - 1))}…`;
}

function assignmentDetail(assignment) {
  if (assignment.retrying) {
    return '执行器正在恢复子进程';
  }
  if (assignment.error) return truncate(assignment.error, 96);
  if (assignment.summary) return truncate(assignment.summary, 96);
  return '';
}

function executionSummary(assignment) {
  const stats = assignment.executionStats || {};
  if (!stats.loopTurns && !stats.providerRequests && !stats.toolCallsExecuted) return '';
  return `工具回合 ${stats.loopTurns || 0} · 模型请求 ${stats.providerRequests || 0} · 已执行工具 ${stats.toolCallsExecuted || 0}`;
}

function recentOutput(assignment) {
  const text = String(assignment.error || assignment.summary || '').trim();
  if (!text) return '';
  return text.length > 280 ? `${text.slice(0, 280)}…` : text;
}

export function WorkerPanel({
  assignments = [],
  onCancelAssignment,
  onOpenAssignment,
}) {
  const [expandedId, setExpandedId] = useState('');
  const activeCount = assignments.filter((item) => isActiveStatus(item.status)).length;

  function toggleExpanded(id) {
    setExpandedId((current) => (current === id ? '' : id));
  }

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
                const expanded = expandedId === assignment.id;
                const output = recentOutput(assignment);
                const metrics = executionSummary(assignment);
                return (
                  <article
                    className={classNames(
                      'worker-item',
                      active && 'is-running',
                      failed && 'is-failed',
                      expanded && 'is-expanded',
                      `status-${status}`,
                    )}
                    data-status={status}
                    data-testid="worker-assignment"
                    key={assignment.id}
                  >
                    <div className="worker-item-main-row">
                      <span
                        className={classNames('worker-icon', active && 'is-active', failed && 'is-failed')}
                        aria-hidden="true"
                      >
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
                          {shortId(assignment.workerId) || '等待 Worker'}
                          {assignment.attempt > 1 ? ` · 尝试 ${assignment.attempt}` : ''}
                        </span>
                        {metrics ? <small className="worker-detail">{metrics}</small> : null}
                        {detail && !expanded ? (
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
                          aria-expanded={expanded}
                          data-testid="worker-assignment-expand"
                          label={expanded ? '收起详情' : '展开详情'}
                          onClick={() => toggleExpanded(assignment.id)}
                        >
                          {expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
                        </IconButton>
                        <IconButton
                          data-testid="worker-assignment-cancel"
                          disabled={!canCancel}
                          label={canCancel ? '取消工作分配' : '当前工作分配不可取消'}
                          onClick={() => onCancelAssignment?.(assignment)}
                        >
                          <CircleStop size={15} />
                        </IconButton>
                      </div>
                    </div>

                    {expanded ? (
                      <div className="worker-item-detail" data-testid="worker-assignment-detail">
                        <dl className="worker-detail-grid">
                          <div>
                            <dt>任务</dt>
                            <dd>{String(assignment.task || '').trim() || '未提供任务描述'}</dd>
                          </div>
                          <div>
                            <dt>父运行</dt>
                            <dd title={assignment.runId || ''}>
                              {shortId(assignment.runId, 16) || '—'}
                              {assignment.runId ? ` · ${status}` : ''}
                            </dd>
                          </div>
                          <div>
                            <dt>Worker</dt>
                            <dd title={assignment.workerId || ''}>
                              {assignment.workerId || '尚未分配'}
                            </dd>
                          </div>
                          {metrics ? (
                            <div>
                              <dt>执行统计</dt>
                              <dd>
                                {metrics}
                                {assignment.executionStats?.maxTurnsReached ? ' · 已达回合上限' : ''}
                                {assignment.executionStats?.toolBudgetReached ? ' · 已达工具上限' : ''}
                              </dd>
                            </div>
                          ) : null}
                        </dl>
                        {output ? (
                          <div className={classNames('worker-output-preview', failed && 'is-error')}>
                            <strong>{failed ? '错误 / 最近输出' : '最近输出'}</strong>
                            <pre>{output}</pre>
                          </div>
                        ) : (
                          <p className="worker-output-empty">暂无最近输出摘要。</p>
                        )}
                        <div className="worker-detail-actions">
                          <Button
                            data-testid="worker-assignment-open-full"
                            icon={<ExternalLink size={14} />}
                            onClick={() => onOpenAssignment?.(assignment)}
                            variant="soft"
                          >
                            打开完整对话
                          </Button>
                          <Button
                            disabled={!canCancel}
                            onClick={() => onCancelAssignment?.(assignment)}
                            variant="ghost"
                          >
                            取消
                          </Button>
                        </div>
                      </div>
                    ) : null}
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
