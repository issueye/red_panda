/**
 * Session Goal helpers (long-horizon objectives).
 */

export const GOAL_STATUS_LABELS = {
  pending: '待启动',
  active: '进行中',
  paused: '已暂停',
  succeeded: '已达成',
  failed: '未达成',
  cancelled: '已取消',
};

export const GOAL_PHASE_LABELS = {
  analyze: '分析',
  plan: '规划',
  execute: '执行',
  verify: '验证',
  evaluate: '终评',
  report: '报告',
};

export const GOAL_PAUSE_REASON_LABELS = {
  awaiting_continue: '自动续跑',
  user_cancel: '用户停止',
  session_compact: '会话摘要',
  run_failed: '运行失败',
  permission_denied: '权限拒绝',
  stale_run: '运行中断',
  fork: '会话分叉',
  budget_exhausted: '预算耗尽',
};

/**
 * @param {any} raw
 */
export function normalizeGoal(raw = {}) {
  return {
    id: raw.id || '',
    sessionId: raw.session_id || raw.sessionId || '',
    title: raw.title || '',
    objective: raw.objective || '',
    successCriteria: raw.success_criteria || raw.successCriteria || '',
    status: String(raw.status || 'pending').toLowerCase(),
    pipelinePhase: raw.pipeline_phase || raw.pipelinePhase || '',
    pauseReason: raw.pause_reason || raw.pauseReason || '',
    failReason: raw.fail_reason || raw.failReason || '',
    analysisSummary: raw.analysis_summary || raw.analysisSummary || '',
    checkpointSummary: raw.checkpoint_summary || raw.checkpointSummary || '',
    progressNote: raw.progress_note || raw.progressNote || '',
    reportMarkdown: raw.report_markdown || raw.reportMarkdown || '',
    usedToolTurns: Number(raw.used_tool_turns ?? raw.usedToolTurns ?? 0),
    maxTotalToolTurns: Number(raw.max_total_tool_turns ?? raw.maxTotalToolTurns ?? 0),
    usedSegments: Number(raw.used_segments ?? raw.usedSegments ?? 0),
    maxSegmentsPerRun: Number(raw.max_segments_per_run ?? raw.maxSegmentsPerRun ?? 0),
    maxToolTurnsSeg: Number(raw.max_tool_turns_per_segment ?? raw.maxToolTurnsSeg ?? 0),
    activeRunId: raw.active_run_id || raw.activeRunId || '',
    lastRunId: raw.last_run_id || raw.lastRunId || '',
    updatedAt: raw.updated_at || raw.updatedAt || '',
  };
}

/**
 * @param {any} data
 * @returns {ReturnType<typeof normalizeGoal>[]}
 */
export function normalizeGoalList(data) {
  if (Array.isArray(data)) return data.map(normalizeGoal);
  if (Array.isArray(data?.items)) return data.items.map(normalizeGoal);
  return [];
}

/**
 * Focus goal for the strip: prefer active, else latest paused, else latest non-terminal.
 * @param {ReturnType<typeof normalizeGoal>[]} items
 */
export function pickFocusGoal(items = []) {
  if (!items.length) return null;
  const active = items.find((g) => g.status === 'active');
  if (active) return active;
  const paused = items.find((g) => g.status === 'paused');
  if (paused) return paused;
  const pending = items.find((g) => g.status === 'pending');
  if (pending) return pending;
  return items[0];
}

export function goalStatusLabel(status) {
  return GOAL_STATUS_LABELS[status] || status || '未知';
}

export function goalPhaseLabel(phase) {
  return GOAL_PHASE_LABELS[phase] || phase || '';
}

export function goalPauseReasonLabel(reason) {
  return GOAL_PAUSE_REASON_LABELS[reason] || reason || '';
}

export function goalDisplayTitle(goal) {
  if (!goal) return '';
  if (goal.title) return goal.title;
  const obj = String(goal.objective || '').trim();
  if (!obj) return goal.id || '目标';
  return obj.length > 40 ? `${obj.slice(0, 40)}…` : obj;
}

export function goalCanContinue(goal) {
  if (!goal) return false;
  if (goalShouldAutoContinue(goal)) return false;
  return goal.status === 'paused' || goal.status === 'pending';
}

export function goalShouldAutoContinue(goal) {
  if (!goal || goal.status !== 'paused' || goal.pauseReason !== 'awaiting_continue') return false;
  return goal.maxTotalToolTurns <= 0 || goal.usedToolTurns < goal.maxTotalToolTurns;
}

export function goalCanCancel(goal) {
  if (!goal) return false;
  return goal.status === 'active' || goal.status === 'paused' || goal.status === 'pending';
}

/**
 * @param {any} body goal_updated payload
 */
export function goalFromUpdatedEvent(body = {}) {
  const raw = body.goal || body;
  if (!raw || typeof raw !== 'object') return null;
  return normalizeGoal(raw);
}

/** Compact bar label: tool-turn budget only (O5a keeps advanced counters collapsed). */
export function formatGoalBudget(goal) {
  if (!goal) return '';
  const used = goal.usedToolTurns || 0;
  const max = goal.maxTotalToolTurns || 0;
  if (max > 0) return `${used}/${max} 轮`;
  if (used > 0) return `${used} 轮`;
  return '';
}

/**
 * Expanded-only budget details (segments / per-segment caps).
 * Auto-continue and wall-time knobs stay out of the default strip.
 */
export function formatGoalBudgetAdvanced(goal) {
  if (!goal) return '';
  const parts = [];
  const segUsed = goal.usedSegments || 0;
  const segMax = goal.maxSegmentsPerRun || 0;
  if (segMax > 0 || segUsed > 0) {
    parts.push(segMax > 0 ? `段 ${segUsed}/${segMax}` : `段 ${segUsed}`);
  }
  const perSeg = goal.maxToolTurnsSeg || 0;
  if (perSeg > 0) {
    parts.push(`每段≤${perSeg} 轮`);
  }
  return parts.join(' · ');
}
