import { Activity, Braces, CheckCircle2, ChevronDown, ChevronRight, Clock3, KeyRound, Loader2, Search, ShieldAlert, Wrench, XCircle } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  displayRisk,
  displayEventKind,
  displayRuntimeMode,
  displayStatus,
} from '../lib/displayLabels.js';
import {
  filterRunEvents,
  formatRunEventPayload,
  getRunEventFilterOptions,
  getRunEventTimelineMeta,
  groupRunEventsByKind,
} from '../lib/activityEvents.js';
import { formatSeq } from '../lib/format.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button } from './ui/button.jsx';
import { ErrorMessage, InlineEmpty } from './ui/feedback.jsx';
import { PanelHeader } from './ui/panel.jsx';
import { SelectMenu } from './ui/select.jsx';

function statusIcon(status) {
  if (status === 'completed') return <CheckCircle2 size={14} />;
  if (status === 'failed' || status === 'denied' || status === 'cancelled') return <XCircle size={14} />;
  if (status === 'waiting_permission') return <ShieldAlert size={14} />;
  return <Loader2 size={14} />;
}

function compactTime(value) {
  if (!value) return '未记录';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '未记录';
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function matchesQuery(run, query) {
  if (!query) return true;
  const haystack = [
    run.id,
    run.input,
    run.status,
    run.lastEventType,
    run.error,
  ].join(' ').toLowerCase();
  return haystack.includes(query.toLowerCase());
}

function runStatusMatches(run, filter) {
  if (filter === 'all') return true;
  if (filter === 'active') return run.status === 'running' || run.status === 'waiting_permission';
  return run.status === filter;
}

export function RunActivityPanel({
  currentRunId,
  globalPendingPermissions,
  onLoadRunEvents,
  onResolvePermission,
  permissions,
  runEventsByRun,
  runEventsError,
  runEventsLoading,
  runs,
  tools,
}) {
  const safeRuns = Array.isArray(runs) ? runs : [];
  const safeTools = Array.isArray(tools) ? tools : [];
  const safePermissions = Array.isArray(permissions) ? permissions : [];
  const safeGlobalPending = Array.isArray(globalPendingPermissions) ? globalPendingPermissions : [];
  const activeRuns = safeRuns.filter((run) => run.status === 'running' || run.status === 'waiting_permission');
  const pendingPermissions = safePermissions.filter((item) => item.status === 'pending' || !item.status);
  const globalPendingCount = safeGlobalPending.length;
  const latestRun = safeRuns[0];
  const [query, setQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [eventKindFilter, setEventKindFilter] = useState('all');
  const [eventScopeFilter, setEventScopeFilter] = useState('all');
  const [expandedPayloads, setExpandedPayloads] = useState({});
  const [expandedRunId, setExpandedRunId] = useState(currentRunId || '');
  const filteredRuns = useMemo(() => (
    safeRuns.filter((run) => runStatusMatches(run, statusFilter) && matchesQuery(run, query))
  ), [query, safeRuns, statusFilter]);
  useEffect(() => {
    if (expandedRunId && !runEventsByRun?.[expandedRunId] && !runEventsLoading?.[expandedRunId]) {
      onLoadRunEvents?.(expandedRunId);
    }
  }, [expandedRunId, onLoadRunEvents, runEventsByRun, runEventsLoading]);

  useEffect(() => {
    setEventKindFilter('all');
    setEventScopeFilter('all');
    setExpandedPayloads({});
  }, [expandedRunId]);

  function toggleRun(runId, expanded) {
    const next = expanded ? '' : runId;
    setExpandedRunId(next);
    if (next && !runEventsByRun?.[next]) {
      onLoadRunEvents?.(next);
    }
  }

  function togglePayload(eventId) {
    setExpandedPayloads((current) => ({
      ...current,
      [eventId]: !current[eventId],
    }));
  }

  return (
    <section className="activity-panel-content" data-testid="activity-panel">
      <PanelHeader title="运行活动" />

      <div className="activity-summary-grid">
        <article className="activity-summary-card">
          <Activity size={15} />
          <div>
            <strong>{activeRuns.length}</strong>
            <span>运行中</span>
          </div>
        </article>
        <article className="activity-summary-card">
          <Wrench size={15} />
          <div>
            <strong>{safeTools.length}</strong>
            <span>工具</span>
          </div>
        </article>
        <article className="activity-summary-card">
          <KeyRound size={15} />
          <div>
            <strong>{globalPendingCount}</strong>
            <span>待处理</span>
          </div>
        </article>
      </div>

      {globalPendingCount > 0 ? (
        <div className="activity-section">
          <div className="activity-section-title">
            <KeyRound size={14} />
            <span>全局待处理</span>
          </div>
          <div className="activity-list compact">
            {safeGlobalPending.slice(0, 6).map((item) => (
              <article className="activity-line urgent" key={item.id}>
                <div>
                  <strong>{item.summary || item.toolName || '需要授权'}</strong>
                  <span>{item.toolName || (item.risk ? `${displayRisk(item.risk)}风险` : '运行授权')} - {item.runId || '未知运行'}</span>
                </div>
                <div className="activity-actions">
                  <Button onClick={() => onResolvePermission?.(item.id, 'deny')} variant="ghost">拒绝</Button>
                  <Button onClick={() => onResolvePermission?.(item.id, 'approve')} variant="soft">允许</Button>
                </div>
              </article>
            ))}
          </div>
        </div>
      ) : null}

      <div className="activity-section">
        <div className="activity-filter-bar">
          <label className="activity-search">
            <Search size={14} />
            <input
              onChange={(event) => setQuery(event.target.value)}
              placeholder="搜索运行"
              type="search"
              value={query}
            />
          </label>
          <SelectMenu
            ariaLabel="运行状态筛选"
            className="activity-filter-select"
            onChange={setStatusFilter}
            options={[
              ['all', '全部'],
              ['active', '进行中'],
              ['completed', '已完成'],
              ['cancelled', '已取消'],
              ['failed', '失败'],
              ['waiting_permission', '等待授权'],
            ]}
            value={statusFilter}
          />
        </div>

        <div className="activity-section-title">
          <Clock3 size={14} />
          <span>运行记录</span>
          <em>显示 {filteredRuns.length} 条</em>
        </div>
        <div className="activity-list">
          {safeRuns.length === 0 ? (
            <InlineEmpty className="activity-empty">当前会话暂无运行记录。</InlineEmpty>
          ) : filteredRuns.length === 0 ? (
            <InlineEmpty className="activity-empty">没有符合当前筛选条件的运行。</InlineEmpty>
          ) : filteredRuns.slice(0, 12).map((run) => {
            const runTools = safeTools.filter((tool) => tool.rootRunId === run.id);
            const runPermissions = safePermissions.filter((item) => item.runId === run.id);
            const runEvents = runEventsByRun?.[run.id] || [];
            const eventsLoading = Boolean(runEventsLoading?.[run.id]);
            const eventsError = runEventsError?.[run.id] || '';
            const expanded = expandedRunId === run.id;
            const eventOptions = getRunEventFilterOptions(runEvents);
            const filteredEvents = filterRunEvents(runEvents, {
              kind: eventKindFilter,
              scope: eventScopeFilter,
            });
            const eventGroups = groupRunEventsByKind(filteredEvents);
            return (
              <article className={run.id === currentRunId ? 'activity-run active' : 'activity-run'} data-testid="activity-run" key={run.id}>
                <button
                  className="activity-run-toggle"
                  data-testid="activity-run-toggle"
                  onClick={() => toggleRun(run.id, expanded)}
                  type="button"
                >
                  {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  <span>{expanded ? '收起' : '展开'}</span>
                </button>
                <div className="activity-row-head">
                  <StatusBadge
                    className={`activity-status activity-status-${run.status || 'unknown'}`}
                    data-testid="activity-run-status"
                    icon={statusIcon(run.status)}
                    status={run.status || 'unknown'}
                  />
                  <span>seq {formatSeq(run.lastRootSeq || 0)}</span>
                </div>
                <strong>{run.input || run.id}</strong>
                <div className="activity-row-meta">
                  <span>{run.messageCount || 0} 条消息</span>
                  <span>{runTools.length || run.toolCount || 0} 个工具</span>
                  <span>{runPermissions.length} 条授权</span>
                  <span>{compactTime(run.updatedAt || run.startedAt)}</span>
                </div>
                <ErrorMessage className="activity-error">{run.error}</ErrorMessage>
                {expanded ? (
                  <div className="activity-run-detail">
                    <dl>
                      <div><dt>运行</dt><dd>{run.id}</dd></div>
                      <div><dt>最近事件</dt><dd>{run.lastEventType || '未记录'}</dd></div>
                      <div><dt>运行时</dt><dd>{displayRuntimeMode(run.runtimeMode || 'single_core')}</dd></div>
                      <div><dt>开始</dt><dd>{compactTime(run.startedAt)}</dd></div>
                    </dl>
                    <div className="activity-detail-group">
                      <strong>工具</strong>
                      {runTools.length === 0 ? <span>本次运行没有工具调用。</span> : runTools.map((tool) => (
                        <p key={tool.id}>{tool.displayName || tool.name} - {displayStatus(tool.status || 'running')} - seq {formatSeq(tool.rootSeq || 0)}</p>
                      ))}
                    </div>
                    <div className="activity-detail-group">
                      <strong>授权</strong>
                      {runPermissions.length === 0 ? <span>本次运行没有授权记录。</span> : runPermissions.map((item) => (
                        <p key={item.id}>{item.summary || item.toolName || item.id} - {displayStatus(item.status || 'pending')}</p>
                      ))}
                    </div>
                    <div className="activity-detail-group" data-testid="activity-event-timeline">
                      <div className="activity-detail-title-row">
                        <strong>事件时间线</strong>
                        <span data-testid="activity-event-count">{filteredEvents.length}/{runEvents.length}</span>
                      </div>
                      <div className="activity-event-controls">
                        <SelectMenu
                          ariaLabel="事件类型筛选"
                          testId="activity-event-kind-filter"
                          onChange={setEventKindFilter}
                          options={eventOptions.kinds.map((kind) => [
                            kind,
                            kind === 'all' ? '全部类型' : displayEventKind(kind),
                          ])}
                          value={eventKindFilter}
                        />
                        <SelectMenu
                          ariaLabel="事件代理筛选"
                          testId="activity-event-agent-filter"
                          onChange={setEventScopeFilter}
                          options={eventOptions.scopes.map((scope) => [
                            scope,
                            scope === 'all' ? '全部代理' : scope,
                          ])}
                          value={eventScopeFilter}
                        />
                      </div>
                      {eventsLoading ? <span>正在加载事件...</span> : null}
                      <ErrorMessage className="activity-error">{eventsError}</ErrorMessage>
                      {!eventsLoading && !eventsError && runEvents.length === 0 ? <span>本次运行暂无事件。</span> : null}
                      {!eventsLoading && !eventsError && runEvents.length > 0 && filteredEvents.length === 0 ? <span>没有符合当前筛选条件的事件。</span> : null}
                      {eventGroups.map((group) => (
                        <div className="activity-event-group" data-testid="activity-event-group" key={group.kind}>
                          <div className="activity-event-group-head">
                            <span className={`activity-event-kind activity-event-kind-${group.kind}`}>{displayEventKind(group.kind)}</span>
                            <em>{group.count}</em>
                          </div>
                          {group.events.slice(0, 16).map((event) => {
                            const meta = getRunEventTimelineMeta(event);
                            const payload = formatRunEventPayload(event);
                            const eventKey = event.id || `${event.rootRunId || run.id}:${event.rootSeq || 0}:${event.agentSeq || 0}:${event.type || 'event'}`;
                            const payloadOpen = Boolean(expandedPayloads[eventKey]);
                            return (
                              <div className="activity-event-row" key={eventKey}>
                                <div className="activity-event-meta">
                                  <span>{meta.sequence || `root#${formatSeq(event.rootSeq)}`}</span>
                                  <span>{meta.scope}</span>
                                  {payload ? (
                                    <button
                                      aria-expanded={payloadOpen}
                                      className="activity-event-payload-toggle"
                                      data-testid="activity-event-payload-toggle"
                                      onClick={() => togglePayload(eventKey)}
                                      title="载荷"
                                      type="button"
                                    >
                                      <Braces size={11} />
                                    </button>
                                  ) : null}
                                </div>
                                <p>
                                  <strong>{meta.title}</strong>
                                  <span>{meta.summary}</span>
                                </p>
                                {payloadOpen ? <pre data-testid="activity-event-payload">{payload}</pre> : null}
                              </div>
                            );
                          })}
                        </div>
                      ))}
                    </div>
                  </div>
                ) : null}
              </article>
            );
          })}
        </div>
      </div>

      <div className="activity-section">
        <div className="activity-section-title">
          <Wrench size={14} />
          <span>工具调用</span>
        </div>
        <div className="activity-list compact">
          {safeTools.length === 0 ? (
            <InlineEmpty className="activity-empty">暂无工具调用。</InlineEmpty>
          ) : safeTools.slice(0, 8).map((tool) => (
            <article className="activity-line" key={tool.id}>
              <div>
                <strong>{tool.displayName || tool.name}</strong>
                <span>{tool.name} - {displayRisk(tool.risk || 'low')}风险</span>
              </div>
              <StatusBadge className={`activity-badge activity-badge-${tool.status || 'running'}`} status={tool.status || 'running'} />
            </article>
          ))}
        </div>
      </div>

      <div className="activity-section">
        <div className="activity-section-title">
          <ShieldAlert size={14} />
          <span>授权</span>
        </div>
        <div className="activity-list compact">
          {safePermissions.length === 0 ? (
            <InlineEmpty className="activity-empty">暂无授权记录。</InlineEmpty>
          ) : safePermissions.slice(0, 6).map((item) => (
            <article className="activity-line" key={item.id}>
              <div>
                <strong>{item.summary || item.toolName || '需要授权'}</strong>
                <span>{item.toolName || (item.risk ? `${displayRisk(item.risk)}风险` : '运行授权')}</span>
              </div>
              <StatusBadge className={`activity-badge activity-badge-${item.status || 'pending'}`} status={item.status || 'pending'} />
            </article>
          ))}
        </div>
      </div>

      {latestRun ? (
        <div className="activity-footnote">
          最近运行更新于 {compactTime(latestRun.updatedAt || latestRun.startedAt)}。
        </div>
      ) : null}
    </section>
  );
}
