import { displaySessionKind, displayStatus } from './displayLabels.js';

/**
 * Gateway DTO → Desktop session list item.
 * @param {any} session
 */
export function normalizeSession(session) {
  if (!session?.id && !session?.name) {
    return null;
  }
  const kind = session.kind && session.kind !== 'normal' ? session.kind : '';
  // BREAKING single-path DTO: name + workspace_root only (no title/working_dir aliases).
  const workspaceRoot = session.workspace_root || '';
  const detail = workspaceRoot || displayStatus(session.status || 'active');
  const kindLabel = displaySessionKind(kind);
  return {
    id: session.id,
    title: session.name || session.id,
    subtitle: kind ? `${kindLabel || kind} - ${detail}` : detail,
    kind,
    parentId: session.parent_id || '',
    workspaceRoot,
  };
}

/**
 * @param {any} item
 */
export function normalizeWorkspace(item) {
  if (!item) return null;
  return {
    id: item.id,
    root: item.root || item.root_path || '',
    root_path: item.root_path || item.root || '',
    name: item.name || '',
    lastOpenedAt: item.last_opened_at,
  };
}

/**
 * @param {any} message
 */
export function normalizeHistoryMessage(message) {
  const firstText = Array.isArray(message.content)
    ? message.content.find((item) => item.type === 'text')?.text
    : '';
  return {
    id: message.id,
    role: message.role === 'user' ? 'user' : 'assistant',
    messageSeq: message.seq || 0,
    runId: message.run_id || '',
    assignmentId: message.assignment_id || '',
    workerId: message.worker_id || '',
    profileKey: message.profile_key || '',
    runSeq: message.run_seq || 0,
    visibility: message.visibility || 'run_public',
    createdAt: message.created_at,
    text: firstText || '',
    attachments: Array.isArray(message.attachments) ? message.attachments : [],
  };
}

/**
 * @param {any} item
 */
export function normalizeToolCall(item) {
  return {
    id: item.id,
    runId: item.run_id,
    assignmentId: item.assignment_id,
    workerId: item.worker_id,
    profileKey: item.profile_key || '',
    name: item.tool_name || 'tool',
    displayName: item.display_name || item.tool_name || '工具',
    risk: item.risk || 'low',
    arguments: item.arguments || {},
    status: item.status || 'running',
    output: item.output || '',
    error: item.error || '',
    durationMs: item.duration_ms,
    runSeq: item.run_seq || 0,
    startedAt: item.started_at,
  };
}

/**
 * @param {any} item
 */
export function normalizeRun(item) {
  return {
    id: item.id,
    sessionId: item.session_id,
    workspaceRoot: item.workspace_root || '',
    runtimeMode: item.runtime_mode || 'single_core',
    status: item.status || 'unknown',
    input: item.input || '',
    lastEventType: item.last_event_type || '',
    lastRunSeq: item.last_run_seq || 0,
    messageCount: item.message_count || 0,
    toolCount: item.tool_count || 0,
    error: item.error || '',
    startedAt: item.started_at,
    finishedAt: item.finished_at,
    updatedAt: item.updated_at,
  };
}

/**
 * @param {any} item
 */
export function normalizePermission(item) {
  return {
    id: item.id,
    runId: item.run_id,
    assignmentId: item.assignment_id || '',
    workerId: item.worker_id || '',
    profileKey: item.profile_key || '',
    status: item.status || 'pending',
    decision: item.decision || '',
    summary: item.summary || '需要授权',
    detail: item.detail || item.tool_name || '系统正在等待处理决定。',
    risk: item.risk,
    toolName: item.tool_name,
    arguments: item.arguments || {},
    runSeq: item.run_seq || 0,
    createdAt: item.created_at,
  };
}

/**
 * Prefer the newest running / waiting_permission run from raw Gateway rows.
 * @param {any[]} runs
 */
export function latestActiveRun(runs) {
  if (!Array.isArray(runs)) return null;
  return runs
    .filter((item) => item.status === 'running' || item.status === 'waiting_permission')
    .sort((a, b) => new Date(b.updated_at || b.started_at || 0) - new Date(a.updated_at || a.started_at || 0))[0] || null;
}

/**
 * @param {any[]} runs
 * @param {number} fallback
 */
export function latestRunSeq(runs, fallback) {
  if (!Array.isArray(runs)) return fallback;
  return runs.reduce((max, item) => Math.max(max, item.last_run_seq || 0), fallback);
}
