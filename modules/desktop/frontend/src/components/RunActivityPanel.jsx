import { Activity, Braces, CheckCircle2, ChevronDown, ChevronRight, Clock3, KeyRound, Loader2, Search, ShieldAlert, Wrench, XCircle } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  filterRunEvents,
  formatRunEventPayload,
  getRunEventFilterOptions,
  getRunEventTimelineMeta,
  groupRunEventsByKind,
} from '../lib/activityEvents.js';
import { formatSeq } from '../lib/format.js';
import { Button } from './ui/button.jsx';

function statusIcon(status) {
  if (status === 'completed') return <CheckCircle2 size={14} />;
  if (status === 'failed' || status === 'denied' || status === 'cancelled') return <XCircle size={14} />;
  if (status === 'waiting_permission') return <ShieldAlert size={14} />;
  return <Loader2 size={14} />;
}

function compactTime(value) {
  if (!value) return 'not recorded';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'not recorded';
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
      <div className="panel-header">
        <div>
          <strong>Run Activity</strong>
          <span>Session runs, tools, and approvals</span>
        </div>
      </div>

      <div className="activity-summary-grid">
        <article className="activity-summary-card">
          <Activity size={15} />
          <div>
            <strong>{activeRuns.length}</strong>
            <span>active</span>
          </div>
        </article>
        <article className="activity-summary-card">
          <Wrench size={15} />
          <div>
            <strong>{safeTools.length}</strong>
            <span>tools</span>
          </div>
        </article>
        <article className="activity-summary-card">
          <KeyRound size={15} />
          <div>
            <strong>{globalPendingCount}</strong>
            <span>pending</span>
          </div>
        </article>
      </div>

      {globalPendingCount > 0 ? (
        <div className="activity-section">
          <div className="activity-section-title">
            <KeyRound size={14} />
            <span>Global Pending</span>
          </div>
          <div className="activity-list compact">
            {safeGlobalPending.slice(0, 6).map((item) => (
              <article className="activity-line urgent" key={item.id}>
                <div>
                  <strong>{item.summary || item.toolName || 'Permission required'}</strong>
                  <span>{item.toolName || item.risk || 'runtime approval'} - {item.runId || 'run unknown'}</span>
                </div>
                <div className="activity-actions">
                  <Button onClick={() => onResolvePermission?.(item.id, 'deny')} variant="ghost">Deny</Button>
                  <Button onClick={() => onResolvePermission?.(item.id, 'approve')} variant="soft">Allow</Button>
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
              placeholder="Search runs"
              type="search"
              value={query}
            />
          </label>
          <select
            aria-label="Run status filter"
            className="activity-filter-select"
            onChange={(event) => setStatusFilter(event.target.value)}
            value={statusFilter}
          >
            <option value="all">All</option>
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="cancelled">Cancelled</option>
            <option value="failed">Failed</option>
            <option value="waiting_permission">Waiting</option>
          </select>
        </div>

        <div className="activity-section-title">
          <Clock3 size={14} />
          <span>Runs</span>
          <em>{filteredRuns.length} shown</em>
        </div>
        <div className="activity-list">
          {safeRuns.length === 0 ? (
            <p className="activity-empty">No runs recorded for this session.</p>
          ) : filteredRuns.length === 0 ? (
            <p className="activity-empty">No runs match the current filter.</p>
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
                  <span>{expanded ? 'Hide' : 'Show'}</span>
                </button>
                <div className="activity-row-head">
                  <div className={`activity-status activity-status-${run.status || 'unknown'}`} data-testid="activity-run-status">
                    {statusIcon(run.status)}
                    <span>{run.status || 'unknown'}</span>
                  </div>
                  <span>seq {formatSeq(run.lastRootSeq || 0)}</span>
                </div>
                <strong>{run.input || run.id}</strong>
                <div className="activity-row-meta">
                  <span>{run.messageCount || 0} messages</span>
                  <span>{runTools.length || run.toolCount || 0} tools</span>
                  <span>{runPermissions.length} permissions</span>
                  <span>{compactTime(run.updatedAt || run.startedAt)}</span>
                </div>
                {run.error ? <p className="activity-error">{run.error}</p> : null}
                {expanded ? (
                  <div className="activity-run-detail">
                    <dl>
                      <div><dt>Run</dt><dd>{run.id}</dd></div>
                      <div><dt>Last event</dt><dd>{run.lastEventType || 'not recorded'}</dd></div>
                      <div><dt>Runtime</dt><dd>{run.runtimeMode || 'single_core'}</dd></div>
                      <div><dt>Started</dt><dd>{compactTime(run.startedAt)}</dd></div>
                    </dl>
                    <div className="activity-detail-group">
                      <strong>Tools</strong>
                      {runTools.length === 0 ? <span>No tools for this run.</span> : runTools.map((tool) => (
                        <p key={tool.id}>{tool.displayName || tool.name} - {tool.status || 'running'} - seq {formatSeq(tool.rootSeq || 0)}</p>
                      ))}
                    </div>
                    <div className="activity-detail-group">
                      <strong>Permissions</strong>
                      {runPermissions.length === 0 ? <span>No permission records for this run.</span> : runPermissions.map((item) => (
                        <p key={item.id}>{item.summary || item.toolName || item.id} - {item.status || 'pending'}</p>
                      ))}
                    </div>
                    <div className="activity-detail-group" data-testid="activity-event-timeline">
                      <div className="activity-detail-title-row">
                        <strong>Event Timeline</strong>
                        <span data-testid="activity-event-count">{filteredEvents.length}/{runEvents.length}</span>
                      </div>
                      <div className="activity-event-controls">
                        <select
                          aria-label="Event kind filter"
                          data-testid="activity-event-kind-filter"
                          onChange={(event) => setEventKindFilter(event.target.value)}
                          value={eventKindFilter}
                        >
                          {eventOptions.kinds.map((kind) => (
                            <option key={kind} value={kind}>{kind === 'all' ? 'All kinds' : kind}</option>
                          ))}
                        </select>
                        <select
                          aria-label="Event agent filter"
                          data-testid="activity-event-agent-filter"
                          onChange={(event) => setEventScopeFilter(event.target.value)}
                          value={eventScopeFilter}
                        >
                          {eventOptions.scopes.map((scope) => (
                            <option key={scope} value={scope}>{scope === 'all' ? 'All agents' : scope}</option>
                          ))}
                        </select>
                      </div>
                      {eventsLoading ? <span>Loading events...</span> : null}
                      {eventsError ? <p className="activity-error">{eventsError}</p> : null}
                      {!eventsLoading && !eventsError && runEvents.length === 0 ? <span>No events loaded for this run.</span> : null}
                      {!eventsLoading && !eventsError && runEvents.length > 0 && filteredEvents.length === 0 ? <span>No events match the current filters.</span> : null}
                      {eventGroups.map((group) => (
                        <div className="activity-event-group" data-testid="activity-event-group" key={group.kind}>
                          <div className="activity-event-group-head">
                            <span className={`activity-event-kind activity-event-kind-${group.kind}`}>{group.kind}</span>
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
                                      title="Payload"
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
          <span>Tool Calls</span>
        </div>
        <div className="activity-list compact">
          {safeTools.length === 0 ? (
            <p className="activity-empty">No tool calls yet.</p>
          ) : safeTools.slice(0, 8).map((tool) => (
            <article className="activity-line" key={tool.id}>
              <div>
                <strong>{tool.displayName || tool.name}</strong>
                <span>{tool.name} - {tool.risk || 'low'} risk</span>
              </div>
              <span className={`activity-badge activity-badge-${tool.status || 'running'}`}>
                {tool.status || 'running'}
              </span>
            </article>
          ))}
        </div>
      </div>

      <div className="activity-section">
        <div className="activity-section-title">
          <ShieldAlert size={14} />
          <span>Permissions</span>
        </div>
        <div className="activity-list compact">
          {safePermissions.length === 0 ? (
            <p className="activity-empty">No permission records loaded.</p>
          ) : safePermissions.slice(0, 6).map((item) => (
            <article className="activity-line" key={item.id}>
              <div>
                <strong>{item.summary || item.toolName || 'Permission required'}</strong>
                <span>{item.toolName || item.risk || 'runtime approval'}</span>
              </div>
              <span className={`activity-badge activity-badge-${item.status || 'pending'}`}>
                {item.status || 'pending'}
              </span>
            </article>
          ))}
        </div>
      </div>

      {latestRun ? (
        <div className="activity-footnote">
          Latest run updated {compactTime(latestRun.updatedAt || latestRun.startedAt)}.
        </div>
      ) : null}
    </section>
  );
}
