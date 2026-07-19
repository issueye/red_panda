/**
 * Per-session desktop runtime projection.
 * Enables concurrent multi-session runs without sharing one global chat state.
 */

/**
 * @param {Partial<ReturnType<typeof createEmptySessionRuntime>>} [overrides]
 */
export function createEmptySessionRuntime(overrides = {}) {
  return {
    messages: [],
    tools: [],
    permissions: [],
    runs: [],
    assignmentsById: {},
    assignmentOrder: [],
    running: false,
    cancelRequested: false,
    currentRunId: '',
    runSeq: 0,
    // run_seq restarts at 1 for every Run; never compare it across Runs.
    runSeqByRun: {},
    runEventsByRun: {},
    runEventsLoading: {},
    runEventsError: {},
    draft: '',
    hydrated: false,
    todos: [],
    todoOpenCount: 0,
    todosVersion: 0,
    todosHydrated: false,
    todosExpanded: false,
    todosAutoExpandedOnce: false,
    contextSummary: null,
    contextSummaryEndSeq: 0,
    // UI message index covered by the active in-place summary (for live rows without messageSeq).
    contextSummaryCoveredCount: 0,
    // Keep-tail turns used when the summary was applied (fallback boundary without seqs).
    contextSummaryKeepTailTurns: 0,
    // True while this session is pausing runs and applying a context summary.
    compacting: false,
    // Long-horizon Goal strip (docs/32 M5).
    goal: null,
    goals: [],
    goalHydrated: false,
    goalExpanded: false,
    goalBusy: false,
    conversationTabs: [{
      id: 'main', kind: 'main', title: '主对话', closable: false,
    }],
    activeConversationTab: 'main',
    ...overrides,
  };
}

/**
 * @param {Record<string, ReturnType<typeof createEmptySessionRuntime>>} map
 * @param {string} sessionId
 */
export function getSessionRuntime(map, sessionId) {
  if (!sessionId) return createEmptySessionRuntime();
  return map[sessionId] || createEmptySessionRuntime();
}

/**
 * @param {Record<string, ReturnType<typeof createEmptySessionRuntime>>} map
 * @param {string} sessionId
 * @param {object | ((prev: object) => object)} patch
 */
export function patchSessionRuntimeMap(map, sessionId, patch) {
  if (!sessionId) return map;
  const prev = map[sessionId] || createEmptySessionRuntime();
  const next = typeof patch === 'function' ? patch(prev) : { ...prev, ...patch };
  if (next === prev) return map;
  return { ...map, [sessionId]: next };
}

/**
 * Resolve which session an event belongs to.
 * @param {object} eventPayload v0.2 run event envelope (requires session_id)
 */
export function resolveEventSessionId(eventPayload) {
  return eventPayload?.session_id || '';
}

/**
 * Aggregate resume cursors across all sessions for websocket resume.
 * @param {Record<string, object>} map
 */
export function collectResumeCursors(map) {
  const cursors = {};
  for (const runtime of Object.values(map || {})) {
    for (const run of runtime.runs || []) {
      if (run.status === 'running' || run.status === 'waiting_permission') {
        cursors[run.id] = Math.max(
          Number(cursors[run.id]) || 0,
          Number(runtime.runSeqByRun?.[run.id]) || 0,
          Number(run.lastRunSeq) || 0,
        );
      }
    }
    if (runtime.running && runtime.currentRunId) {
      cursors[runtime.currentRunId] = Math.max(
        Number(cursors[runtime.currentRunId]) || 0,
        Number(runtime.runSeqByRun?.[runtime.currentRunId]) || 0,
      );
    }
  }
  return cursors;
}

/**
 * @param {Record<string, object>} map
 * @returns {Record<string, 'running' | 'waiting_permission' | 'idle'>}
 */
export function collectSessionRunStatus(map) {
  const statusBySession = {};
  for (const [sessionId, runtime] of Object.entries(map || {})) {
    if ((runtime.permissions || []).some((item) => item.status === 'pending')) {
      statusBySession[sessionId] = 'waiting_permission';
      continue;
    }
    if (runtime.running || (runtime.runs || []).some((run) => run.status === 'running' || run.status === 'waiting_permission')) {
      statusBySession[sessionId] = 'running';
      continue;
    }
    statusBySession[sessionId] = 'idle';
  }
  return statusBySession;
}

/**
 * Count active runs across sessions.
 * @param {Record<string, object>} map
 */
export function countActiveRuns(map) {
  let count = 0;
  for (const runtime of Object.values(map || {})) {
    if (runtime.running) count += 1;
    else if ((runtime.runs || []).some((run) => run.status === 'running' || run.status === 'waiting_permission')) {
      count += 1;
    }
  }
  return count;
}

/** Max idle (non-running, non-focused) session runtimes retained in memory (docs/48 D). */
export const MAX_IDLE_SESSION_RUNTIMES = 12;

function sessionRuntimeIsActive(runtime) {
  if (!runtime) return false;
  if (runtime.running) return true;
  if ((runtime.permissions || []).some((item) => item.status === 'pending')) return true;
  return (runtime.runs || []).some((run) => run.status === 'running' || run.status === 'waiting_permission');
}

/**
 * Drop least-recently-touched idle session projections to bound Desktop memory.
 * Always keeps keepSessionId and any actively running sessions.
 *
 * @param {Record<string, object>} map
 * @param {{ keepSessionId?: string, maxIdle?: number }} [options]
 */
export function pruneIdleSessionRuntimes(map, { keepSessionId = '', maxIdle = MAX_IDLE_SESSION_RUNTIMES } = {}) {
  const entries = Object.entries(map || {});
  if (entries.length === 0) return map || {};

  const protectedIds = new Set();
  if (keepSessionId) protectedIds.add(keepSessionId);
  for (const [id, runtime] of entries) {
    if (sessionRuntimeIsActive(runtime)) protectedIds.add(id);
  }

  const idle = entries
    .filter(([id]) => !protectedIds.has(id))
    .sort((a, b) => (Number(b[1]?.lastTouchedAt) || 0) - (Number(a[1]?.lastTouchedAt) || 0));

  if (idle.length <= maxIdle) return map;

  const drop = new Set(idle.slice(maxIdle).map(([id]) => id));
  const next = { ...map };
  for (const id of drop) {
    delete next[id];
  }
  return next;
}
