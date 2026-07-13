import { useCallback, useEffect, useRef } from 'react';
import { apiJson } from '../lib/api.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import {
  goalShouldAutoContinue,
  normalizeGoalList,
  pickFocusGoal,
} from '../lib/goals.js';
import { buildRunStartOptions } from '../lib/runOptions.js';
import {
  countOpenTodos,
  normalizeTodo,
} from '../lib/todos.js';

/**
 * Goal/Todo hydrate + continue/cancel + auto-continue (docs/36 C3).
 * Session Goal strip state remains a Gateway projection: hydrate + reduce only.
 *
 * Pure-ish helpers below are exported for unit tests that drive the same paths
 * the hook uses (injectable fetchJson / request).
 */

/**
 * @param {string} sessionId
 * @param {(sessionId: string, updater: (prev: any) => any) => void} patchRuntime
 * @param {typeof apiJson} [fetchJson]
 */
export async function hydrateSessionTodos(sessionId, patchRuntime, fetchJson = apiJson) {
  if (!sessionId) return;
  try {
    const data = await fetchJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/todos`);
    const items = Array.isArray(data?.items)
      ? data.items.map(normalizeTodo)
      : Array.isArray(data)
        ? data.map(normalizeTodo)
        : [];
    const openCount = Number(data?.open_count ?? countOpenTodos(items));
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      todos: items,
      todoOpenCount: openCount,
      todosHydrated: true,
      todosVersion: (prev.todosVersion || 0) + 1,
    }));
  } catch {
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      todosHydrated: true,
    }));
  }
}

/**
 * @param {string} sessionId
 * @param {(sessionId: string, updater: (prev: any) => any) => void} patchRuntime
 * @param {typeof apiJson} [fetchJson]
 */
export async function hydrateSessionGoals(sessionId, patchRuntime, fetchJson = apiJson) {
  if (!sessionId || sessionId === 'local-design') return;
  try {
    const data = await fetchJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/goals`);
    const items = normalizeGoalList(data);
    const focus = pickFocusGoal(items);
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      goals: items,
      goal: focus,
      goalHydrated: true,
      goalExpanded: focus && (focus.status === 'active' || focus.status === 'paused')
        ? (prev.goalExpanded || false)
        : prev.goalExpanded,
    }));
  } catch {
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      goalHydrated: true,
    }));
  }
}

/**
 * @param {object} params
 * @param {string} params.sessionId
 * @param {{ id: string }} params.goal
 * @param {string} [params.extraText]
 * @param {object} [params.optionsOverrides]
 * @param {object} params.runSettings
 * @param {object|null} params.workspace
 * @param {(sessionId: string, updater: (prev: any) => any) => void} params.patchRuntime
 * @param {(sessionId: string) => Promise<void>} params.hydrateGoals
 * @param {typeof apiJson} [params.fetchJson]
 */
export async function continueSessionGoal({
  sessionId,
  goal,
  extraText = '',
  optionsOverrides = {},
  runSettings,
  workspace,
  patchRuntime,
  hydrateGoals,
  fetchJson = apiJson,
}) {
  if (!sessionId || !goal?.id) {
    throw new Error('当前没有可继续的目标');
  }
  patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: true }));
  try {
    const result = await fetchJson(
      `/api/v1/sessions/${encodeURIComponent(sessionId)}/goals/${encodeURIComponent(goal.id)}/continue`,
      {
        method: 'POST',
        body: JSON.stringify({
          input: extraText || '',
          options: buildRunStartOptions(runSettings, workspace, '', optionsOverrides),
        }),
      },
    );
    const nextRunId = result?.run_id || '';
    appendDiagnosticLog('info', `已继续目标 ${goal.id}`, {
      source: 'goal',
      detail: { sessionId, goalId: goal.id, runId: nextRunId },
    });
    patchRuntime(sessionId, (rt) => ({
      ...rt,
      goalBusy: false,
      running: true,
      currentRunId: nextRunId || rt.currentRunId,
      draft: '',
    }));
    await hydrateGoals(sessionId);
    return result;
  } catch (error) {
    appendDiagnosticLog('error', `继续目标失败：${error.message}`, { source: 'goal' });
    patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: false }));
    throw error;
  }
}

/**
 * @param {object} params
 * @param {string} params.sessionId
 * @param {{ id: string }} params.goal
 * @param {string} [params.runId]
 * @param {(method: string, params: object) => Promise<any>} [params.request]
 * @param {(sessionId: string, updater: (prev: any) => any) => void} params.patchRuntime
 * @param {(sessionId: string) => Promise<void>} params.hydrateGoals
 * @param {typeof apiJson} [params.fetchJson]
 */
export async function cancelSessionGoal({
  sessionId,
  goal,
  runId,
  request,
  patchRuntime,
  hydrateGoals,
  fetchJson = apiJson,
}) {
  if (!sessionId || !goal?.id) {
    throw new Error('当前没有可取消的目标');
  }
  patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: true }));
  try {
    // If a run is active for this session, cancel it first (also pauses goal server-side).
    if (runId && typeof request === 'function') {
      await request('run.cancel', { run_id: runId, reason: 'goal cancelled' }).catch(() => null);
    }
    await fetchJson(
      `/api/v1/sessions/${encodeURIComponent(sessionId)}/goals/${encodeURIComponent(goal.id)}/cancel`,
      { method: 'POST', body: '{}' },
    );
    await hydrateGoals(sessionId);
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      goalBusy: false,
      running: false,
      currentRunId: '',
    }));
  } catch (error) {
    appendDiagnosticLog('error', `取消目标失败：${error.message}`, { source: 'goal' });
    patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: false }));
    throw error;
  }
}

/** Stable key for auto-continue dedupe (one attempt per goal version). */
export function autoContinueDedupeKey(sessionId, goal) {
  if (!sessionId || !goal?.id) return '';
  return `${sessionId}:${goal.id}:${goal.updatedAt || goal.usedToolTurns}`;
}

/**
 * Gate for auto-continue effect (before dedupe set).
 * Policy itself stays in lib/goals.js goalShouldAutoContinue.
 */
export function shouldAttemptAutoContinue({
  sessionId,
  goal,
  running,
  compacting,
  goalBusy,
}) {
  if (!sessionId || sessionId === 'local-design') return false;
  if (!goalShouldAutoContinue(goal) || running || compacting || goalBusy) return false;
  return true;
}

/**
 * @param {object} options
 * @param {(sessionId: string, updater: (prev: any) => any) => void} options.patchRuntime
 * @param {string} options.currentSessionId
 * @param {{ current: string }} options.currentSessionIdRef
 * @param {{ current: Record<string, any> }} options.sessionRuntimesRef
 * @param {object} options.runSettings
 * @param {object|null} options.workspace
 * @param {(method: string, params: object) => Promise<any>} options.request
 * @param {any} options.goal
 * @param {boolean} options.running
 * @param {boolean} options.compacting
 * @param {boolean} options.goalBusy
 */
export function useGoalSession({
  patchRuntime,
  currentSessionId,
  currentSessionIdRef,
  sessionRuntimesRef,
  runSettings,
  workspace,
  request,
  goal,
  running,
  compacting,
  goalBusy,
}) {
  const autoContinuedGoalsRef = useRef(new Set());

  const hydrateTodos = useCallback(async (sessionId) => {
    await hydrateSessionTodos(sessionId, patchRuntime);
  }, [patchRuntime]);

  const hydrateGoals = useCallback(async (sessionId) => {
    await hydrateSessionGoals(sessionId, patchRuntime);
  }, [patchRuntime]);

  const continueGoal = useCallback(async (extraText = '', optionsOverrides = {}) => {
    const sessionId = currentSessionIdRef.current;
    const current = sessionRuntimesRef.current[sessionId]?.goal;
    return continueSessionGoal({
      sessionId,
      goal: current,
      extraText,
      optionsOverrides,
      runSettings,
      workspace,
      patchRuntime,
      hydrateGoals,
    });
  }, [
    currentSessionIdRef,
    hydrateGoals,
    patchRuntime,
    runSettings,
    sessionRuntimesRef,
    workspace,
  ]);

  const cancelGoal = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const rt = sessionRuntimesRef.current[sessionId];
    const current = rt?.goal;
    return cancelSessionGoal({
      sessionId,
      goal: current,
      runId: rt?.currentRunId,
      request,
      patchRuntime,
      hydrateGoals,
    });
  }, [
    currentSessionIdRef,
    hydrateGoals,
    patchRuntime,
    request,
    sessionRuntimesRef,
  ]);

  useEffect(() => {
    if (!shouldAttemptAutoContinue({
      sessionId: currentSessionId,
      goal,
      running,
      compacting,
      goalBusy,
    })) {
      return;
    }

    const key = autoContinueDedupeKey(currentSessionId, goal);
    if (!key || autoContinuedGoalsRef.current.has(key)) return;
    autoContinuedGoalsRef.current.add(key);
    appendDiagnosticLog('info', `Goal 自动续跑 ${goal.id}`, {
      source: 'goal',
      detail: { sessionId: currentSessionId, goalId: goal.id, reason: goal.pauseReason },
    });
    continueGoal('', { goals_enabled: true }).catch(() => {
      // The key prevents a render loop. A later server update gets a new key.
    });
  }, [compacting, continueGoal, currentSessionId, goal, goalBusy, running]);

  return {
    hydrateTodos,
    hydrateGoals,
    continueGoal,
    cancelGoal,
  };
}
