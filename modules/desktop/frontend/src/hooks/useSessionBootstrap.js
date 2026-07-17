import { useCallback, useEffect, useState } from 'react';
import { apiJson } from '../lib/api.js';
import { normalizeGoalList, pickFocusGoal } from '../lib/goals.js';
import {
  createEmptySessionRuntime,
} from '../lib/sessionRuntime.js';
import {
  latestActiveRun,
  latestRunSeq,
  normalizeHistoryMessage,
  normalizePermission,
  normalizeRun,
  normalizeSession,
  normalizeToolCall,
  normalizeWorkspace,
} from '../lib/sessionNormalize.js';
import {
  countOpenTodos,
  normalizeTodo,
} from '../lib/todos.js';

/** Empty until Gateway bootstrap; no fake local-design session (docs/45). */
export const INITIAL_BOOTSTRAP_SESSION_ID = '';

/**
 * Bootstrap + session list + workspace open/hydrate (docs/36 C1).
 * Session run projection (sessionRuntimes) stays owned by App; this hook only
 * patches it via the provided patchRuntime helper.
 *
 * @param {{
 *   patchRuntime: (sessionId: string, updater: (prev: any) => any) => void,
 *   workspaceRootRef: { current: string },
 *   loadProviderProfiles: () => Promise<any>,
 *   loadWorkerProfiles: () => Promise<any>,
 *   loadMcpServers: () => Promise<any>,
 *   loadSkills: (workspaceRoot?: string) => Promise<any>,
 * }} options
 */
export function useSessionBootstrap({
  patchRuntime,
  workspaceRootRef,
  loadProviderProfiles,
  loadWorkerProfiles,
  loadMcpServers,
  loadSkills,
}) {
  const [sessions, setSessions] = useState([]);
  const [currentSessionId, setCurrentSessionId] = useState('');
  const [workspace, setWorkspace] = useState(null);
  const [recentWorkspaces, setRecentWorkspaces] = useState([]);
  const [globalPendingPermissions, setGlobalPendingPermissions] = useState([]);

  // Keep Gateway resource loaders in sync with the active workspace root.
  if (workspaceRootRef) {
    workspaceRootRef.current = workspace?.root_path || workspace?.root || '';
  }

  const loadGlobalPendingPermissions = useCallback(async () => {
    const items = await apiJson('/api/v1/permissions/pending').catch(() => []);
    setGlobalPendingPermissions(Array.isArray(items) ? items.map(normalizePermission) : []);
  }, []);

  const loadSessionState = useCallback(async (sessionId, { preserveLive = true } = {}) => {
    try {
      const [history, serverRuns, toolCalls, permissionItems, todoData, compactionData, goalData] = await Promise.all([
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/history`),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/runs`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/tools`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/permissions`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/todos`).catch(() => null),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact`).catch(() => null),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/goals`).catch(() => null),
      ]);
      const normalized = Array.isArray(history) ? history.map(normalizeHistoryMessage) : [];
      const normalizedRuns = Array.isArray(serverRuns) ? serverRuns.map(normalizeRun) : [];
      const activeRun = latestActiveRun(serverRuns);
      const runSeqByRun = Object.fromEntries(normalizedRuns.map((run) => [run.id, Number(run.lastRunSeq) || 0]));
      const todoItems = Array.isArray(todoData?.items)
        ? todoData.items.map(normalizeTodo)
        : [];
      const todoOpen = Number(todoData?.open_count ?? countOpenTodos(todoItems));
      const compactSummary = compactionData?.active ? (compactionData.summary || null) : null;
      const compactEndSeq = compactionData?.active
        ? Number(compactionData.compaction?.source_end_seq) || 0
        : 0;
      const goalItems = goalData ? normalizeGoalList(goalData) : [];
      const focusGoal = goalData ? pickFocusGoal(goalItems) : null;
      patchRuntime(sessionId, (prev) => {
        // Keep in-memory live projection when switching back to a still-running session.
        if (preserveLive && prev.hydrated && prev.running) {
          const activeRunId = activeRun?.id || prev.currentRunId || '';
          const activeRunSeq = Math.max(
            Number(runSeqByRun[activeRunId]) || 0,
            Number(prev.runSeqByRun?.[activeRunId]) || 0,
          );
          return {
            ...prev,
            runs: normalizedRuns.length > 0 ? normalizedRuns : prev.runs,
            currentRunId: activeRunId,
            runSeq: activeRunSeq,
            runSeqByRun: {
              ...runSeqByRun,
              ...(prev.runSeqByRun || {}),
              ...(activeRunId ? { [activeRunId]: activeRunSeq } : {}),
            },
            todos: todoData ? todoItems : prev.todos,
            todoOpenCount: todoData ? todoOpen : prev.todoOpenCount,
            todosHydrated: true,
            goals: goalData ? goalItems : prev.goals,
            goal: goalData ? focusGoal : prev.goal,
            goalHydrated: true,
            contextSummary: compactSummary,
            contextSummaryEndSeq: compactEndSeq,
            // History messages carry messageSeq; coveredCount is only needed for live rows.
            contextSummaryCoveredCount: 0,
            contextSummaryKeepTailTurns: compactEndSeq > 0 ? 3 : 0,
            hydrated: true,
          };
        }
        return {
          ...prev,
          messages: normalized,
          tools: Array.isArray(toolCalls) ? toolCalls.map(normalizeToolCall) : [],
          permissions: Array.isArray(permissionItems)
            ? permissionItems.map(normalizePermission)
            : [],
          runs: normalizedRuns,
          running: Boolean(activeRun) || prev.running,
          currentRunId: activeRun?.id || prev.currentRunId || '',
          runSeq: activeRun
            ? Number(activeRun.last_run_seq) || 0
            : latestRunSeq(serverRuns, 0),
          runSeqByRun,
          todos: todoItems,
          todoOpenCount: todoOpen,
          todosHydrated: true,
          todosExpanded: false,
          todosAutoExpandedOnce: false,
          goals: goalItems,
          goal: focusGoal,
          goalHydrated: true,
          goalExpanded: false,
          goalBusy: false,
          contextSummary: compactSummary,
          contextSummaryEndSeq: compactEndSeq,
          contextSummaryCoveredCount: 0,
          contextSummaryKeepTailTurns: compactEndSeq > 0 ? 3 : 0,
          hydrated: true,
        };
      });
      loadGlobalPendingPermissions();
    } catch {
      patchRuntime(sessionId, () => createEmptySessionRuntime({ hydrated: true }));
      setGlobalPendingPermissions([]);
    }
  }, [loadGlobalPendingPermissions, patchRuntime]);

  const openWorkspaceRoot = useCallback(async (root) => {
    if (!root) return null;
    const currentRoot = workspace?.root_path || workspace?.root || '';
    if (currentRoot && currentRoot.replace(/\\/g, '/').toLowerCase() === root.replace(/\\/g, '/').toLowerCase()) {
      return workspace;
    }
    const opened = await apiJson('/api/v1/workspaces/open', {
      method: 'POST',
      body: JSON.stringify({ root }),
    });
    const normalized = normalizeWorkspace(opened);
    setWorkspace(normalized);
    setRecentWorkspaces((items) => {
      const next = [normalized, ...items.filter((item) => item.id !== normalized.id && item.root !== normalized.root)];
      return next.slice(0, 20);
    });
    if (typeof loadSkills === 'function') {
      loadSkills(normalized?.root_path || normalized?.root || '');
    }
    return normalized;
  }, [loadSkills, workspace]);

  const selectWorkspaceNode = useCallback(async (node) => {
    if (!node?.root) return;
    await openWorkspaceRoot(node.root);
  }, [openWorkspaceRoot]);

  // App bootstrap: workspace + sessions + settings resource caches.
  useEffect(() => {
    let alive = true;
    apiJson('/api/v1/app/bootstrap')
      .then((data) => {
        if (!alive) return;
        const nextWorkspace = normalizeWorkspace(data.workspace);
        setWorkspace(nextWorkspace);
        const recent = Array.isArray(data.recent_workspaces)
          ? data.recent_workspaces.map(normalizeWorkspace).filter(Boolean)
          : [];
        setRecentWorkspaces(recent);
        const nextSessions = Array.isArray(data.sessions)
          ? data.sessions.map(normalizeSession).filter(Boolean)
          : [];
        setSessions(nextSessions);
        if (nextSessions.length > 0) {
          setCurrentSessionId(nextSessions[0].id);
          loadSessionState(nextSessions[0].id);
        } else {
          setCurrentSessionId('');
          loadGlobalPendingPermissions();
        }
        loadProviderProfiles?.();
        loadWorkerProfiles?.();
        loadMcpServers?.();
        loadSkills?.(nextWorkspace?.root_path || nextWorkspace?.root || '');
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
    // Mount-only: resource loaders are stable enough for cold start.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    sessions,
    setSessions,
    currentSessionId,
    setCurrentSessionId,
    workspace,
    setWorkspace,
    recentWorkspaces,
    setRecentWorkspaces,
    globalPendingPermissions,
    setGlobalPendingPermissions,
    loadSessionState,
    loadGlobalPendingPermissions,
    openWorkspaceRoot,
    selectWorkspaceNode,
  };
}
