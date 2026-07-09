import { displayEventKind, displayRisk, displayStatus } from './displayLabels.js';

export function normalizeRunEvent(item) {
  const type = item.type || 'event';
  const agentRole = item.agent_role || item.agent?.role || 'root';
  const payload = item.payload || {};
  const streamKind = item.stream_kind || item.stream?.kind || '';
  const eventKind = item.event_kind || item.kind || classifyRunEventKind(type, streamKind, payload);

  return {
    id: item.id || item.event_id,
    type,
    eventKind,
    rootRunId: item.root_run_id || '',
    runId: item.run_id || '',
    parentRunId: item.parent_run_id || '',
    sessionId: item.session_id || '',
    rootSeq: item.root_seq || 0,
    agentSeq: item.agent_seq || 0,
    agentId: item.agent_id || item.agent?.agent_id || '',
    agentRole,
    agentName: item.agent_name || item.agent?.name || item.agent_id || 'agent',
    streamKind,
    payload,
    createdAt: item.created_at,
  };
}

export function summarizeRunEvent(event) {
  const payload = event?.payload || {};
  const kind = event?.eventKind || classifyRunEventKind(event?.type, event?.streamKind, payload);

  if (kind === 'tool') {
    const name = firstPresent(payload.tool_name, payload.toolName, payload.name, payload.display_name);
    const status = firstPresent(payload.status, payload.state, payload.result_status);
    const input = summarizeToolInput(payload);
    return trimSummary(joinParts([name || '工具', status ? displayStatus(status) : '', input]));
  }

  if (kind === 'permission') {
    const action = firstPresent(payload.action, payload.decision, payload.status, payload.state);
    const toolName = firstPresent(payload.tool_name, payload.toolName, payload.name);
    const reason = firstPresent(payload.summary, payload.reason, payload.risk ? `${displayRisk(payload.risk)}风险` : '');
    return trimSummary(joinParts(['授权', action ? displayStatus(action) : '', toolName, reason]));
  }

  if (kind === 'error') {
    return trimSummary(firstPresent(payload.error, payload.message, payload.summary, event?.type, '错误'));
  }

  if (kind === 'done') {
    const status = firstPresent(payload.status, payload.state, payload.reason);
    return trimSummary(joinParts(['完成', status ? displayStatus(status) : '']));
  }

  if (payload.delta) return trimSummary(payload.delta);
  if (payload.message) return trimSummary(payload.message);
  if (payload.summary) return trimSummary(payload.summary);
  if (payload.status) return trimSummary(displayStatus(payload.status));
  return trimSummary(event?.streamKind || event?.agentName || displayEventKind('event'));
}

export function getRunEventTimelineMeta(event) {
  const kind = event?.eventKind || classifyRunEventKind(event?.type, event?.streamKind, event?.payload || {});
  return {
    kind,
    scope: formatRunEventScope(event),
    sequence: formatRunEventSequence(event),
    title: event?.type || kind || 'event',
    summary: summarizeRunEvent(event),
  };
}

export function filterRunEvents(events, filters = {}) {
  const kind = filters.kind || 'all';
  const scope = filters.scope || 'all';
  return sortRunEvents(safeEvents(events)).filter((event) => {
    const meta = getRunEventTimelineMeta(event);
    if (kind !== 'all' && meta.kind !== kind) {
      return false;
    }
    if (scope !== 'all' && meta.scope !== scope) {
      return false;
    }
    return true;
  });
}

export function groupRunEventsByKind(events) {
  const groups = new Map();
  for (const event of sortRunEvents(safeEvents(events))) {
    const meta = getRunEventTimelineMeta(event);
    if (!groups.has(meta.kind)) {
      groups.set(meta.kind, {
        kind: meta.kind,
        count: 0,
        events: [],
      });
    }
    const group = groups.get(meta.kind);
    group.count += 1;
    group.events.push(event);
  }
  return Array.from(groups.values()).sort((left, right) => (
    eventKindRank(left.kind) - eventKindRank(right.kind)
  ));
}

export function getRunEventFilterOptions(events) {
  const kinds = new Set();
  const scopes = new Set();
  for (const event of safeEvents(events)) {
    const meta = getRunEventTimelineMeta(event);
    kinds.add(meta.kind);
    scopes.add(meta.scope);
  }
  return {
    kinds: ['all', ...Array.from(kinds).sort((left, right) => eventKindRank(left) - eventKindRank(right))],
    scopes: ['all', ...Array.from(scopes).sort()],
  };
}

export function formatRunEventPayload(event) {
  const payload = event?.payload || {};
  if (Object.keys(payload).length === 0) {
    return '';
  }
  const text = JSON.stringify(payload, null, 2);
  if (text.length <= 4000) {
    return text;
  }
  return `${text.slice(0, 4000)}\n... 已截断`;
}

export function classifyRunEventKind(type, streamKind, payload = {}) {
  const value = `${type || ''} ${streamKind || ''}`.toLowerCase();
  if (value.includes('permission') || payload.permission_id || payload.approval_id || payload.decision) {
    return 'permission';
  }
  if (value.includes('memory') || payload.memory_ids) {
    return 'memory';
  }
  if (value.includes('tool') || payload.tool_name || payload.toolName) {
    return 'tool';
  }
  if (value.includes('error') || value.includes('failed') || payload.error) {
    return 'error';
  }
  if (value.includes('done') || value.includes('complete') || value.includes('finish')) {
    return 'done';
  }
  if (value.includes('message') || payload.delta || payload.message) {
    return 'message';
  }
  return 'event';
}

function safeEvents(events) {
  return Array.isArray(events) ? events : [];
}

function sortRunEvents(events) {
  return [...events].sort((left, right) => {
    const leftSeq = Number(left?.rootSeq || 0);
    const rightSeq = Number(right?.rootSeq || 0);
    if (leftSeq !== rightSeq) {
      return leftSeq - rightSeq;
    }
    const leftAgentSeq = Number(left?.agentSeq || 0);
    const rightAgentSeq = Number(right?.agentSeq || 0);
    if (leftAgentSeq !== rightAgentSeq) {
      return leftAgentSeq - rightAgentSeq;
    }
    return new Date(left?.createdAt || 0).getTime() - new Date(right?.createdAt || 0).getTime();
  });
}

function eventKindRank(kind) {
  return {
    error: 0,
    permission: 1,
    memory: 2,
    tool: 3,
    message: 4,
    done: 5,
    event: 6,
    all: -1,
  }[kind] ?? 99;
}

function formatRunEventScope(event) {
  const role = event?.agentRole || 'root';
  if (role === 'root') {
    return 'root';
  }
  if (role === 'subagent') {
    return event?.agentName ? `subagent:${event.agentName}` : event?.agentId ? `subagent:${event.agentId}` : 'subagent';
  }
  if (event?.agentName) {
    return `${role}:${event.agentName}`;
  }
  if (event?.agentId) {
    return `${role}:${event.agentId}`;
  }
  return role;
}

function formatRunEventSequence(event) {
  const rootSeq = Number(event?.rootSeq || 0);
  const agentSeq = Number(event?.agentSeq || 0);
  if (agentSeq > 0 && event?.agentRole && event.agentRole !== 'root') {
    return `root#${rootSeq} agent#${agentSeq}`;
  }
  return `root#${rootSeq}`;
}

function summarizeToolInput(payload) {
  const command = firstPresent(payload.command, payload.cmd);
  if (command) return command;
  const args = firstPresent(payload.args, payload.arguments, payload.input);
  if (!args) return '';
  if (typeof args === 'string') return args;
  if (Array.isArray(args)) return args.join(' ');
  if (typeof args === 'object') {
    return Object.entries(args).slice(0, 2).map(([key, value]) => `${key}=${String(value)}`).join(' ');
  }
  return String(args);
}

function firstPresent(...values) {
  return values.find((value) => value !== undefined && value !== null && String(value).trim() !== '') || '';
}

function joinParts(parts) {
  return parts.filter((part) => part !== undefined && part !== null && String(part).trim() !== '').join(' - ');
}

function trimSummary(value) {
  const text = String(value || '').replace(/\s+/g, ' ').trim();
  if (text.length <= 96) {
    return text;
  }
  return `${text.slice(0, 93)}...`;
}
