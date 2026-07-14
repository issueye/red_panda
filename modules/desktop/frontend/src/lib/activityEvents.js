import { displayEventKind, displayRisk, displayStatus } from './displayLabels.js';

export function normalizeRunEvent(item) {
  const payload = item.payload || {};
  const type = item.type || 'event';
  const worker = item.worker || {};
  return {
    id: item.event_id,
    protocolVersion: item.protocol_version || '',
    type,
    eventKind: item.event_kind || classifyRunEventKind(type, payload),
    runId: item.run_id || '',
    sessionId: item.session_id || '',
    assignmentId: item.assignment_id || '',
    runSeq: Number(item.run_seq) || 0,
    workerSeq: Number(item.worker_seq) || 0,
    workerId: worker.id || '',
    profileKey: worker.profile_key || '',
    payload,
    createdAt: item.created_at,
  };
}

export function summarizeRunEvent(event) {
  const payload = event?.payload || {};
  const kind = event?.eventKind || classifyRunEventKind(event?.type, payload);
  if (event?.type === 'worker_assignment_updated') {
    const attempt = Number(payload.attempt) || 1;
    const status = payload.status ? displayStatus(payload.status) : '更新';
    if (payload.retrying) {
      return trimSummary(`工作分配 - ${status} - 第 ${attempt} 次失败，准备第 ${attempt + 1} 次尝试`);
    }
    return trimSummary(joinParts(['工作分配', status, payload.error || payload.summary || '']));
  }
  if (kind === 'tool') {
    const name = firstPresent(payload.tool_name, payload.name, payload.display_name);
    const status = firstPresent(payload.status, payload.state);
    return trimSummary(joinParts([name || '工具', status ? displayStatus(status) : '', summarizeToolInput(payload)]));
  }
  if (kind === 'permission') {
    const action = firstPresent(payload.action, payload.decision, payload.status);
    const reason = firstPresent(payload.summary, payload.reason, payload.risk ? `${displayRisk(payload.risk)}风险` : '');
    return trimSummary(joinParts(['授权', action ? displayStatus(action) : '', payload.tool_name, reason]));
  }
  if (kind === 'error') return trimSummary(firstPresent(payload.error, payload.message, payload.summary, event?.type, '错误'));
  if (kind === 'done') return trimSummary(joinParts(['完成', payload.status ? displayStatus(payload.status) : '']));
  return trimSummary(firstPresent(payload.delta, payload.message, payload.summary,
    payload.status ? displayStatus(payload.status) : '', event?.workerId, displayEventKind('event')));
}

export function getRunEventTimelineMeta(event) {
  const kind = event?.eventKind || classifyRunEventKind(event?.type, event?.payload || {});
  return {
    kind,
    scope: formatRunEventScope(event),
    sequence: formatRunEventSequence(event),
    title: event?.type || kind || 'event',
    summary: summarizeRunEvent(event),
  };
}

export function filterRunEvents(events, filters = {}) {
  return sortRunEvents(safeEvents(events)).filter((event) => {
    const meta = getRunEventTimelineMeta(event);
    return (!filters.kind || filters.kind === 'all' || meta.kind === filters.kind)
      && (!filters.scope || filters.scope === 'all' || meta.scope === filters.scope);
  });
}

export function groupRunEventsByKind(events) {
  const groups = new Map();
  for (const event of sortRunEvents(safeEvents(events))) {
    const meta = getRunEventTimelineMeta(event);
    const group = groups.get(meta.kind) || { kind: meta.kind, count: 0, events: [] };
    group.count += 1;
    group.events.push(event);
    groups.set(meta.kind, group);
  }
  return Array.from(groups.values()).sort((a, b) => eventKindRank(a.kind) - eventKindRank(b.kind));
}

export function getRunEventFilterOptions(events) {
  const metas = safeEvents(events).map(getRunEventTimelineMeta);
  return {
    kinds: ['all', ...Array.from(new Set(metas.map((item) => item.kind))).sort((a, b) => eventKindRank(a) - eventKindRank(b))],
    scopes: ['all', ...Array.from(new Set(metas.map((item) => item.scope))).sort()],
  };
}

export function formatRunEventPayload(event) {
  const payload = event?.payload || {};
  if (Object.keys(payload).length === 0) return '';
  const text = JSON.stringify(payload, null, 2);
  return text.length <= 4000 ? text : `${text.slice(0, 4000)}\n... 已截断`;
}

export function classifyRunEventKind(type, payload = {}) {
  const value = String(type || '').toLowerCase();
  if (value.includes('permission') || payload.permission_id || payload.decision) return 'permission';
  if (value.includes('memory') || payload.memory_ids) return 'memory';
  if (value.includes('todo')) return 'todo';
  if (value.includes('goal')) return 'goal';
  if (value.includes('tool') || payload.tool_name) return 'tool';
  if (value.includes('error') || value.includes('failed') || payload.error) return 'error';
  if (value.includes('done') || value.includes('complete') || value.includes('finish')) return 'done';
  if (value.includes('message') || payload.delta || payload.message) return 'message';
  return 'event';
}

function safeEvents(events) { return Array.isArray(events) ? events : []; }
function sortRunEvents(events) {
  return [...events].sort((a, b) => a.runSeq - b.runSeq || a.workerSeq - b.workerSeq
    || new Date(a.createdAt || 0).getTime() - new Date(b.createdAt || 0).getTime());
}
function eventKindRank(kind) {
  return ({ all: -1, error: 0, permission: 1, memory: 2, tool: 3, message: 4, done: 5, event: 6 })[kind] ?? 99;
}
function formatRunEventScope(event) {
  if (!event?.workerId) return 'Run';
  return event.profileKey ? `Worker · ${event.workerId} · ${event.profileKey}` : `Worker · ${event.workerId}`;
}
function formatRunEventSequence(event) {
  const run = String(Number(event?.runSeq) || 0).padStart(3, '0');
  const worker = Number(event?.workerSeq) || 0;
  return worker > 0 ? `运行事件 ${run} · Worker 事件 ${String(worker).padStart(3, '0')}` : `运行事件 ${run}`;
}
function summarizeToolInput(payload) {
  const value = firstPresent(payload.command, payload.arguments, payload.input);
  if (!value) return '';
  if (typeof value === 'string') return value;
  if (Array.isArray(value)) return value.join(' ');
  if (typeof value === 'object') return Object.entries(value).slice(0, 2).map(([k, v]) => `${k}=${String(v)}`).join(' ');
  return String(value);
}
function firstPresent(...values) { return values.find((value) => value !== undefined && value !== null && String(value).trim() !== '') || ''; }
function joinParts(parts) { return parts.filter((part) => part !== undefined && part !== null && String(part).trim() !== '').join(' - '); }
function trimSummary(value) {
  const text = String(value || '').replace(/\s+/g, ' ').trim();
  return text.length <= 96 ? text : `${text.slice(0, 93)}...`;
}
