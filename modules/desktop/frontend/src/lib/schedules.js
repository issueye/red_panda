export const emptyScheduleDraft = {
  name: '',
  description: '',
  scheduleKind: 'interval',
  intervalSec: 3600,
  cronExpr: '0 9 * * 1-5',
  runAt: '',
  timezone: 'Asia/Shanghai',
  prompt: '',
  sessionMode: 'new_each_run',
  sessionId: '',
  workspaceRoot: '',
  permissionMode: 'deny',
  toolPolicy: 'allowlist',
  enabled: true,
};

export function normalizeSchedule(item = {}) {
  return {
    id: item.id || '',
    name: item.name || '',
    description: item.description || '',
    enabled: Boolean(item.enabled),
    scheduleKind: item.schedule_kind || 'interval',
    cronExpr: item.cron_expr || '',
    intervalSec: Number(item.interval_sec) || 0,
    runAt: item.run_at || '',
    timezone: item.timezone || '',
    prompt: item.prompt || '',
    runKind: item.run_kind || 'chat',
    sessionMode: item.session_mode || 'new_each_run',
    sessionId: item.session_id || '',
    workspaceRoot: item.workspace_root || '',
    providerProfileId: item.provider_profile_id || '',
    toolPolicy: item.tool_policy || 'allowlist',
    toolAllowlist: Array.isArray(item.tool_allowlist) ? item.tool_allowlist : [],
    toolDenylist: Array.isArray(item.tool_denylist) ? item.tool_denylist : [],
    permissionMode: item.permission_mode || 'deny',
    overlapPolicy: item.overlap_policy || 'skip',
    maxRuns: Number(item.max_runs) || 0,
    runCount: Number(item.run_count) || 0,
    lastRunId: item.last_run_id || '',
    lastStatus: item.last_status || '',
    lastError: item.last_error || '',
    lastFiredAt: item.last_fired_at || '',
    nextRunAt: item.next_run_at || '',
    createdAt: item.created_at || '',
    updatedAt: item.updated_at || '',
  };
}

export function normalizeScheduleRun(item = {}) {
  return {
    id: item.id || '',
    scheduleId: item.schedule_id || '',
    runId: item.run_id || '',
    sessionId: item.session_id || '',
    status: item.status || '',
    skipReason: item.skip_reason || '',
    scheduledFor: item.scheduled_for || '',
    startedAt: item.started_at || '',
    finishedAt: item.finished_at || '',
    error: item.error || '',
    createdAt: item.created_at || '',
  };
}

export function scheduleCreatePayload(draft, context = {}) {
  const kind = draft.scheduleKind || 'interval';
  const payload = {
    name: String(draft.name || '').trim(),
    description: String(draft.description || '').trim(),
    enabled: draft.enabled !== false,
    schedule_kind: kind,
    timezone: draft.timezone || 'Asia/Shanghai',
    prompt: String(draft.prompt || '').trim(),
    session_mode: draft.sessionMode || 'new_each_run',
    workspace_root: draft.workspaceRoot || context.workspaceRoot || '',
    permission_mode: draft.permissionMode || 'deny',
    tool_policy: draft.toolPolicy || 'allowlist',
  };
  if (kind === 'interval') {
    payload.interval_sec = Math.max(60, Number(draft.intervalSec) || 3600);
  } else if (kind === 'cron') {
    payload.cron_expr = String(draft.cronExpr || '').trim();
  } else if (kind === 'one_shot') {
    payload.run_at = String(draft.runAt || '').trim();
  }
  if (payload.session_mode === 'fixed_session' && draft.sessionId) {
    payload.session_id = draft.sessionId;
  }
  if (context.providerProfileId) {
    payload.provider_profile_id = context.providerProfileId;
  }
  return payload;
}

export function scheduleSummary(item) {
  if (!item) return '';
  if (item.scheduleKind === 'cron') return `cron ${item.cronExpr || ''}`.trim();
  if (item.scheduleKind === 'one_shot') return `一次性 ${formatScheduleTime(item.runAt)}`;
  const sec = Number(item.intervalSec) || 0;
  if (sec >= 3600 && sec % 3600 === 0) return `每 ${sec / 3600} 小时`;
  if (sec >= 60 && sec % 60 === 0) return `每 ${sec / 60} 分钟`;
  return `每 ${sec} 秒`;
}

export function formatScheduleTime(value) {
  if (!value) return '—';
  try {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleString();
  } catch {
    return value;
  }
}
