/** Goal V2 projection helpers. */

export const GOAL_STATUS_LABELS = {
  pending: '待启动', active: '推进中', paused: '已暂停',
  succeeded: '已达成', failed: '未达成', cancelled: '已取消',
};

export const GOAL_PAUSE_REASON_LABELS = {
  awaiting_continue: '等待下一轮', user_cancel: '用户停止', session_compact: '会话摘要',
  run_failed: '运行失败', permission_denied: '权限拒绝', stale_run: '运行中断',
  blocked: '需要外部输入', stagnated: '连续无进展', iteration_limit: '迭代上限',
  budget_exhausted: '预算耗尽',
};

export const GOAL_CRITERION_STATUS_LABELS = {
  unknown: '待验证', met: '已满足', not_met: '未满足', blocked: '受阻',
};

export const GOAL_ACTION_STATUS_LABELS = {
  queued: '待执行', active: '执行中', done: '已完成', blocked: '受阻', dropped: '已放弃',
};

export const GOAL_VERDICT_LABELS = {
  progress: '有进展', satisfied: '目标已满足', blocked: '当前受阻', no_progress: '无进展',
};

function normalizeCriterion(raw = {}) {
  return {
    id: raw.id || '', description: raw.description || '',
    status: String(raw.status || 'unknown').toLowerCase(), evidence: raw.evidence || '',
  };
}

function normalizeAction(raw = {}) {
  return {
    id: raw.id || '', key: raw.key || '', title: raw.title || '',
    description: raw.description || '', acceptance: raw.acceptance || '',
    status: String(raw.status || 'queued').toLowerCase(), result: raw.result || '',
    evidence: raw.evidence || '', attempt: Number(raw.attempt || 0),
    sortOrder: Number(raw.sort_order ?? raw.sortOrder ?? 0),
  };
}

function normalizeAssessment(raw) {
  if (!raw || typeof raw !== 'object') return null;
  return {
    verdict: String(raw.verdict || '').toLowerCase(), summary: raw.summary || '',
    gap: raw.gap || '', decision: raw.decision || '', evidence: raw.evidence || '',
    actionId: raw.action_id || raw.actionId || '',
    actionStatus: raw.action_status || raw.actionStatus || '',
    criteria: Array.isArray(raw.criteria) ? raw.criteria.map(normalizeCriterion) : [],
  };
}

export function normalizeGoal(raw = {}) {
  return {
    id: raw.id || '', sessionId: raw.session_id || raw.sessionId || '',
    title: raw.title || '', objective: raw.objective || '',
    status: String(raw.status || 'pending').toLowerCase(),
    pauseReason: raw.pause_reason || raw.pauseReason || '', failReason: raw.fail_reason || raw.failReason || '',
    criteria: Array.isArray(raw.criteria) ? raw.criteria.map(normalizeCriterion) : [],
    constraints: Array.isArray(raw.constraints) ? raw.constraints.map(String) : [],
    strategy: raw.strategy || '', currentActionId: raw.current_action_id || raw.currentActionId || '',
    currentAction: raw.current_action || raw.currentAction || '',
    actions: Array.isArray(raw.actions) ? raw.actions.map(normalizeAction) : [],
    lastObservation: raw.last_observation || raw.lastObservation || '',
    lastAssessment: normalizeAssessment(raw.last_assessment || raw.lastAssessment),
    lastDecision: raw.last_decision || raw.lastDecision || '',
    outcomeSummary: raw.outcome_summary || raw.outcomeSummary || '',
    reportMarkdown: raw.report_markdown || raw.reportMarkdown || '',
    iteration: Number(raw.iteration || 0), maxIterations: Number(raw.max_iterations ?? raw.maxIterations ?? 0),
    stagnationCount: Number(raw.stagnation_count ?? raw.stagnationCount ?? 0),
    maxStagnation: Number(raw.max_stagnation ?? raw.maxStagnation ?? 0),
    usedToolTurns: Number(raw.used_tool_turns ?? raw.usedToolTurns ?? 0),
    maxTotalToolTurns: Number(raw.max_total_tool_turns ?? raw.maxTotalToolTurns ?? 0),
    usedWallTimeSec: Number(raw.used_wall_time_sec ?? raw.usedWallTimeSec ?? 0),
    maxWallTimeSec: Number(raw.max_wall_time_sec ?? raw.maxWallTimeSec ?? 0),
    activeRunId: raw.active_run_id || raw.activeRunId || '', lastRunId: raw.last_run_id || raw.lastRunId || '',
    version: Number(raw.version || 0), updatedAt: raw.updated_at || raw.updatedAt || '',
  };
}

export function normalizeGoalList(data) {
  if (Array.isArray(data)) return data.map(normalizeGoal);
  if (Array.isArray(data?.items)) return data.items.map(normalizeGoal);
  return [];
}

export function pickFocusGoal(items = []) {
  if (!items.length) return null;
  return items.find((goal) => goal.status === 'active')
    || items.find((goal) => goal.status === 'paused')
    || items.find((goal) => goal.status === 'pending')
    || items[0];
}

export function goalStatusLabel(status) { return GOAL_STATUS_LABELS[status] || status || '未知'; }
export function goalPauseReasonLabel(reason) { return GOAL_PAUSE_REASON_LABELS[reason] || reason || ''; }
export function goalCriterionStatusLabel(status) { return GOAL_CRITERION_STATUS_LABELS[status] || status || '待验证'; }
export function goalActionStatusLabel(status) { return GOAL_ACTION_STATUS_LABELS[status] || status || ''; }
export function goalVerdictLabel(verdict) { return GOAL_VERDICT_LABELS[verdict] || verdict || ''; }

export function goalDisplayTitle(goal) {
  if (!goal) return '';
  if (goal.title) return goal.title;
  const objective = String(goal.objective || '').trim();
  if (!objective) return goal.id || '目标';
  return objective.length > 40 ? `${objective.slice(0, 40)}…` : objective;
}

export function goalShouldAutoContinue(goal) {
  if (!goal || goal.status !== 'paused' || goal.pauseReason !== 'awaiting_continue') return false;
  if (goal.lastAssessment?.verdict !== 'progress') return false;
  if (goal.maxIterations > 0 && goal.iteration >= goal.maxIterations) return false;
  if (goal.maxStagnation > 0 && goal.stagnationCount >= goal.maxStagnation) return false;
  return goal.maxTotalToolTurns <= 0 || goal.usedToolTurns < goal.maxTotalToolTurns;
}

export function goalCanContinue(goal) {
  if (!goal || goalShouldAutoContinue(goal)) return false;
  return goal.status === 'paused' || goal.status === 'pending';
}

export function goalCanCancel(goal) {
  return Boolean(goal && ['active', 'paused', 'pending'].includes(goal.status));
}

export function goalFromUpdatedEvent(body = {}) {
  const raw = body.goal || body;
  return raw && typeof raw === 'object' ? normalizeGoal(raw) : null;
}

export function formatGoalBudget(goal) {
  if (!goal) return '';
  const parts = [];
  if (goal.maxIterations > 0) parts.push(`迭代 ${goal.iteration}/${goal.maxIterations}`);
  else if (goal.iteration > 0) parts.push(`迭代 ${goal.iteration}`);
  if (goal.maxTotalToolTurns > 0) parts.push(`工具 ${goal.usedToolTurns}/${goal.maxTotalToolTurns}`);
  return parts.join(' · ');
}

export function formatGoalActionProgress(goal) {
  if (!goal?.actions?.length) return '';
  const done = goal.actions.filter((action) => action.status === 'done').length;
  return `行动 ${done}/${goal.actions.length}`;
}

export function formatGoalControlBudget(goal) {
  if (!goal) return '';
  const parts = [formatGoalBudget(goal)];
  if (goal.maxStagnation > 0) parts.push(`停滞 ${goal.stagnationCount}/${goal.maxStagnation}`);
  if (goal.maxWallTimeSec > 0) parts.push(`耗时 ${goal.usedWallTimeSec}/${goal.maxWallTimeSec}s`);
  return parts.filter(Boolean).join(' · ');
}
