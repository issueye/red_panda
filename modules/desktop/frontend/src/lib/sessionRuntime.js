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
    currentRunId: '',
    runSeq: 0,
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
        cursors[run.id] = Math.max(Number(cursors[run.id]) || 0, Number(run.lastRunSeq) || 0);
      }
    }
    if (runtime.running && runtime.currentRunId) {
      cursors[runtime.currentRunId] = Math.max(
        Number(cursors[runtime.currentRunId]) || 0,
        Number(runtime.runSeq) || 0,
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
