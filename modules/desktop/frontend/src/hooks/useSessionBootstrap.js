import { useCallback, useEffect, useRef, useState } from 'react';
import { apiJson } from '../lib/api.js';
import { normalizeGoalList, pickFocusGoal } from '../lib/goals.js';
import { loadAllSessionHistory } from '../lib/sessionHistory.js';
import { mergeSessionHistoryMessages } from '../lib/sessionMessageMerge.js';
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
  // Per-session hydrate generation: discard stale responses after rapid switches (docs/48 B2).
  const hydrateGenBySessionRef = useRef({});

  // Keep Gateway resource loaders in sync with the active workspace root.
  if (workspaceRootRef) {
    workspaceRootRef.current = workspace?.root_path || workspace?.root || '';
  }

  const loadGlobalPendingPermissions = useCallback(async () => {
    const items = await apiJson('/api/v1/permissions/pending').catch(() => []);
    setGlobalPendingPermissions(Array.isArray(items) ? items.map(normalizePermission) : []);
  }, []);

  const loadSessionState = useCallback(async (sessionId, { preserveLive = true } = {}) => {
    if (!sessionId) return;
    const nextGen = (Number(hydrateGenBySessionRef.current[sessionId]) || 0) + 1;
    hydrateGenBySessionRef.current[sessionId] = nextGen;
    const isStale = () => hydrateGenBySessionRef.current[sessionId] !== nextGen;
    try {
      const [history, serverRuns, toolCalls, permissionItems, todoData, contextData, goalData] = await Promise.all([
        loadAllSessionHistory(sessionId),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/runs`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/tools`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/permissions`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/todos`).catch(() => null),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/context`).catch(() => null),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/goals`).catch(() => null),
      ]);
      if (isStale()) return;
      const normalized = Array.isArray(history) ? history.map(normalizeHistoryMessage) : [];
      const normalizedRuns = Array.isArray(serverRuns) ? serverRuns.map(normalizeRun) : [];
      const activeRun = latestActiveRun(serverRuns);
      const runSeqByRun = Object.fromEntries(normalizedRuns.map((run) => [run.id, Number(run.lastRunSeq) || 0]));
      const todoItems = Array.isArray(todoData?.items)
        ? todoData.items.map(normalizeTodo)
        : [];
      const todoOpen = Number(todoData?.open_count ?? countOpenTodos(todoItems));
      const compactSummary = contextData?.summary_active
        ? (contextData.active_summary?.summary || null)
        : null;
      const compactEndSeq = contextData?.summary_active
        ? Number(contextData.active_summary?.compaction?.source_end_seq) || 0
        : 0;
      const compactKeepTailTurns = contextData?.summary_active
        ? Number(contextData.active_summary?.compaction?.keep_tail_turns) || 0
        : 0;
      const goalItems = goalData ? normalizeGoalList(goalData) : [];
      const focusGoal = goalData ? pickFocusGoal(goalItems) : null;
      const normalizedTools = Array.isArray(toolCalls) ? toolCalls.map(normalizeToolCall) : [];
      const normalizedPermissions = Array.isArray(permissionItems)
        ? permissionItems.map(normalizePermission)
        : [];
      patchRuntime(sessionId, (prev) => {
        if (isStale()) return prev;
        // Live session: merge authoritative history (keep streaming suffix) instead of
        // dropping history refresh (docs/48 Wave B1).
        if (preserveLive && prev.hydrated && prev.running) {
          const activeRunId = activeRun?.id || prev.currentRunId || '';
          const activeRunSeq = Math.max(
            Number(runSeqByRun[activeRunId]) || 0,
            Number(prev.runSeqByRun?.[activeRunId]) || 0,
          );
          return {
            ...prev,
            messages: mergeSessionHistoryMessages(prev.messages, normalized),
            tools: normalizedTools.length > 0 ? normalizedTools : prev.tools,
            permissions: normalizedPermissions.length > 0 ? normalizedPermissions : prev.permissions,
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
            contextSummaryKeepTailTurns: compactKeepTailTurns,
            hydrated: true,
          };
        }
        return {
          ...prev,
          messages: normalized,
          tools: normalizedTools,
          permissions: normalizedPermissions,
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
          contextSummaryKeepTailTurns: compactKeepTailTurns,
          hydrated: true,
        };
      });
      if (!isStale()) {
        loadGlobalPendingPermissions();
      }
    } catch {
      if (isStale()) return;
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
