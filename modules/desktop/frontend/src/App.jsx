import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { ChatPanel } from './components/chat/ChatPanel.jsx';
import { MemoryPanel } from './components/MemoryPanel.jsx';
import { RunActivityPanel } from './components/RunActivityPanel.jsx';
import { SettingsPanel } from './components/SettingsPanel.jsx';
import { Sidebar } from './components/Sidebar.jsx';
import { StatusBar } from './components/StatusBar.jsx';
import { WorkerPanel } from './components/WorkerPanel.jsx';
import { TopBar } from './components/TopBar.jsx';
import { IconButton } from './components/ui/button.jsx';
import { useDialog } from './components/ui/dialog.jsx';
import { TabButton } from './components/ui/tabs.jsx';
import { WorkspacePanel } from './components/WorkspacePanel.jsx';
import { WorkspacePickerDialog } from './components/WorkspacePickerDialog.jsx';
import { useGatewayConnection } from './hooks/useGatewayConnection.js';
import { useGatewayResources } from './hooks/useGatewayResources.js';
import { useGoalSession } from './hooks/useGoalSession.js';
import { useSessionActions } from './hooks/useSessionActions.js';
import {
  INITIAL_BOOTSTRAP_SESSION_ID,
  useSessionBootstrap,
} from './hooks/useSessionBootstrap.js';
import { normalizeRunEvent } from './lib/activityEvents.js';
import { apiJson, gatewayBase } from './lib/api.js';
import { displayRuntimeMode, displayStatus, displayWorkerProfileName } from './lib/displayLabels.js';
import { defaultRunSettings, normalizeStoredRunSettings } from './lib/runOptions.js';
import { isRunTerminalEvent } from './lib/runEventLifecycle.js';
import { reconcileAssignmentsWithRuns, reduceRunEvent, upsertByID } from './lib/reduceRunEvent.js';
import {
  collectResumeCursors,
  collectSessionRunStatus,
  countActiveRuns,
  createEmptySessionRuntime,
  patchSessionRuntimeMap,
  resolveEventSessionId,
} from './lib/sessionRuntime.js';
import {
  estimateEffectiveSessionTokens,
  tokenBudgetState,
} from './lib/tokenBudget.js';

const initialMessages = [
  {
    id: 'm1',
    role: 'assistant',
    agent: 'root',
    runSeq: 1,
    text: '已连接。请输入任务。',
  },
];

const rightPanelTabs = [
  { id: 'workspace', label: '工作区', testId: '' },
  { id: 'workers', label: 'Worker', testId: 'right-tab-workers' },
  { id: 'activity', label: '活动', testId: 'right-tab-activity' },
  { id: 'memory', label: '记忆', testId: 'right-tab-memory' },
];

function loadRunSettings() {
  if (typeof window === 'undefined') {
    return defaultRunSettings;
  }
  try {
    const saved = window.localStorage.getItem('red_panda_run_settings');
    if (!saved) {
      return defaultRunSettings;
    }
    return normalizeStoredRunSettings(JSON.parse(saved));
  } catch {
    return defaultRunSettings;
  }
}

const RIGHT_PANEL_WIDTH_KEY = 'red_panda_right_panel_width';
const RIGHT_PANEL_WIDTH_DEFAULT = 300;
const RIGHT_PANEL_WIDTH_MIN = 220;
const RIGHT_PANEL_WIDTH_MAX = 560;

function clampRightPanelWidth(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return RIGHT_PANEL_WIDTH_DEFAULT;
  return Math.min(RIGHT_PANEL_WIDTH_MAX, Math.max(RIGHT_PANEL_WIDTH_MIN, Math.round(n)));
}

function loadRightPanelWidth() {
  if (typeof window === 'undefined') {
    return RIGHT_PANEL_WIDTH_DEFAULT;
  }
  try {
    return clampRightPanelWidth(window.localStorage.getItem(RIGHT_PANEL_WIDTH_KEY));
  } catch {
    return RIGHT_PANEL_WIDTH_DEFAULT;
  }
}

function normalizeAssignment(item = {}) {
  return {
    id: item.id || '',
    runId: item.run_id || item.runId || '',
    workerId: item.worker_id || item.workerId || '',
    originWorkerId: item.origin_worker_id || item.originWorkerId || '',
    profileKey: item.profile_key || item.profileKey || '',
    task: item.task || '',
    attempt: Number(item.attempt) || 1,
    retryOf: item.retry_of || item.retryOf || '',
    retrying: Boolean(item.retrying),
    retryInMs: Number(item.retry_in_ms ?? item.retryInMs) || 0,
    status: item.status || 'queued',
    result: item.result || '',
    error: item.error || '',
    summary: item.summary || '',
    createdAt: item.created_at || item.createdAt,
    startedAt: item.started_at || item.startedAt,
    finishedAt: item.finished_at || item.finishedAt,
    workerSeq: Number(item.worker_seq ?? item.workerSeq) || 0,
  };
}

export function App() {
  const dialog = useDialog();
  const [sessionRuntimes, setSessionRuntimes] = useState(() => ({
    [INITIAL_BOOTSTRAP_SESSION_ID]: createEmptySessionRuntime({
      messages: initialMessages,
      hydrated: true,
    }),
  }));
  const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false);
  const [rightPanelTab, setRightPanelTab] = useState('workspace');
  const [rightPanelDrawerOpen, setRightPanelDrawerOpen] = useState(false);
  const [workspacePanelExpanded, setWorkspacePanelExpanded] = useState(() => (
    typeof window !== 'undefined' && !window.matchMedia('(max-width: 1100px)').matches
  ));
  const [rightPanelWidth, setRightPanelWidth] = useState(loadRightPanelWidth);
  const [rightPanelResizing, setRightPanelResizing] = useState(false);
  const [compactLayout, setCompactLayout] = useState(() => (
    typeof window !== 'undefined' && window.matchMedia('(max-width: 1100px)').matches
  ));
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [runSettings, setRunSettings] = useState(loadRunSettings);
  const workspaceRootRef = useRef('');
  const getWorkspaceRoot = useCallback(() => workspaceRootRef.current, []);
  const {
    providerProfiles,
    providerProfilesLoading,
    providerProfilesError,
    loadProviderProfiles,
    createProviderProfile,
    updateProviderProfile,
    deleteProviderProfile,
    workerProfiles,
    workerProfilesLoading,
    workerProfilesError,
    loadWorkerProfiles,
    createWorkerProfile,
    updateWorkerProfile,
    deleteWorkerProfile,
    mcpServers,
    mcpServersLoading,
    mcpServersError,
    mcpDiscoveryByServer,
    loadMcpServers,
    createMcpServer,
    updateMcpServer,
    deleteMcpServer,
    discoverMcpServer,
    skills,
    skillsLoading,
    skillsError,
    loadSkills,
    loadSkillDetail,
    createSkill,
    updateSkill,
    deleteSkill,
  } = useGatewayResources({
    getWorkspaceRoot,
    setRunSettings,
  });

  const patchRuntime = useCallback((sessionId, updater) => {
    if (!sessionId) return;
    setSessionRuntimes((map) => patchSessionRuntimeMap(map, sessionId, updater));
  }, []);

  const {
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
  } = useSessionBootstrap({
    patchRuntime,
    workspaceRootRef,
    loadProviderProfiles,
    loadWorkerProfiles,
    loadMcpServers,
    loadSkills,
  });

  const rightPanelCloseRef = useRef(null);
  const rightPanelReturnFocusRef = useRef(null);
  const rightPanelResizeRef = useRef(null);
  const hydrateGoalsRef = useRef(async () => {});
  const currentSessionIdRef = useRef(currentSessionId);
  currentSessionIdRef.current = currentSessionId;
  const sessionRuntimesRef = useRef(sessionRuntimes);
  sessionRuntimesRef.current = sessionRuntimes;

  const runtime = sessionRuntimes[currentSessionId] || createEmptySessionRuntime({
    messages: currentSessionId === INITIAL_BOOTSTRAP_SESSION_ID ? initialMessages : [],
  });
  const {
    messages,
    tools,
    permissions,
    runs,
    assignmentsById = {},
    assignmentOrder = [],
    running,
    currentRunId,
    runSeq,
    runEventsByRun,
    runEventsLoading,
    runEventsError,
    draft,
    todos = [],
    todoOpenCount = 0,
    todosExpanded = false,
    todosHydrated = false,
    contextSummary = null,
    contextSummaryEndSeq = 0,
    compacting = false,
    goal = null,
    goalHydrated = false,
    goalExpanded = false,
    goalBusy = false,
    conversationTabs = [{ id: 'main', kind: 'main', title: '主对话', closable: false }],
    activeConversationTab = 'main',
  } = runtime;

  function patchCurrentRuntime(updater) {
    patchRuntime(currentSessionIdRef.current, updater);
  }

  // UI interactions always target the focused session.
  const bindRuntimeField = (key) => (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    [key]: typeof value === 'function' ? value(rt[key]) : value,
  }));
  const setMessages = bindRuntimeField('messages');
  const setPermissions = bindRuntimeField('permissions');
  const setRuns = bindRuntimeField('runs');
  const setRunEventsByRun = bindRuntimeField('runEventsByRun');
  const setRunEventsLoading = bindRuntimeField('runEventsLoading');
  const setRunEventsError = bindRuntimeField('runEventsError');
  const setDraft = bindRuntimeField('draft');

  const assignments = useMemo(
    () => assignmentOrder.map((id) => assignmentsById[id]).filter(Boolean),
    [assignmentOrder, assignmentsById],
  );
  const displayedConversationTabs = useMemo(() => conversationTabs.map((tab) => {
    if (tab.kind !== 'worker') return tab;
    const assignment = assignmentsById[tab.assignmentId];
    if (!assignment) return tab;
    const title = displayWorkerProfileName(
      assignment.profileKey || assignment.workerId || assignment.id,
    );
    return {
      ...tab,
      title,
      workerId: assignment.workerId || tab.workerId,
      runId: assignment.runId || tab.runId,
      task: assignment.task || tab.task,
      status: assignment.status,
      statusLabel: displayStatus(assignment.status),
    };
  }), [assignmentsById, conversationTabs]);
  const pendingPermissions = useMemo(
    () => permissions.filter((item) => item.status === 'pending' || item.status === 'resolved' || !item.status),
    [permissions],
  );
  const resumeCursors = useMemo(
    () => collectResumeCursors(sessionRuntimes),
    [sessionRuntimes],
  );
  const sessionRunStatus = useMemo(
    () => collectSessionRunStatus(sessionRuntimes),
    [sessionRuntimes],
  );
  const activeRunCount = useMemo(
    () => countActiveRuns(sessionRuntimes),
    [sessionRuntimes],
  );

  const selectedProviderProfile = useMemo(() => {
    if (!runSettings.providerProfileId) {
      return providerProfiles.find((item) => item.isDefault && item.active !== false)
        || providerProfiles.find((item) => item.active !== false)
        || null;
    }
    return providerProfiles.find((item) => item.id === runSettings.providerProfileId) || null;
  }, [providerProfiles, runSettings.providerProfileId]);

  const contextTokenBudget = useMemo(() => {
    const used = estimateEffectiveSessionTokens(messages, draft, tools, {
      endSeq: contextSummaryEndSeq,
      summary: contextSummary,
    });
    const maxTokens = Number(selectedProviderProfile?.maxTokens) || 0;
    return tokenBudgetState(used, maxTokens);
  }, [messages, draft, tools, selectedProviderProfile, contextSummary, contextSummaryEndSeq]);

  useEffect(() => {
    try {
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify(runSettings));
    } catch {
      // Local storage is optional in embedded desktop previews.
    }
  }, [runSettings]);

  useEffect(() => {
    try {
      window.localStorage.setItem(RIGHT_PANEL_WIDTH_KEY, String(rightPanelWidth));
    } catch {
      // Local storage is optional in embedded desktop previews.
    }
  }, [rightPanelWidth]);

  useEffect(() => {
    if (!rightPanelResizing) return undefined;

    function onPointerMove(event) {
      const start = rightPanelResizeRef.current;
      if (!start) return;
      // Drag left = widen right panel.
      const next = clampRightPanelWidth(start.startWidth + (start.startX - event.clientX));
      setRightPanelWidth(next);
    }

    function onPointerUp() {
      setRightPanelResizing(false);
      rightPanelResizeRef.current = null;
    }

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerUp);
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerUp);
    };
  }, [rightPanelResizing]);

  function startRightPanelResize(event) {
    if (compactLayout) return;
    event.preventDefault();
    rightPanelResizeRef.current = {
      startX: event.clientX,
      startWidth: rightPanelWidth,
    };
    setRightPanelResizing(true);
    try {
      event.currentTarget.setPointerCapture?.(event.pointerId);
    } catch {
      // Pointer capture is optional.
    }
  }

  useEffect(() => {
    const media = window.matchMedia('(max-width: 1100px)');
    const handleChange = (event) => {
      setCompactLayout(event.matches);
      if (event.matches) setWorkspacePanelExpanded(false);
      if (!event.matches) setRightPanelDrawerOpen(false);
    };
    setCompactLayout(media.matches);
    media.addEventListener('change', handleChange);
    return () => media.removeEventListener('change', handleChange);
  }, []);

  useEffect(() => {
    if (!compactLayout || !rightPanelDrawerOpen) return undefined;
    const frame = window.requestAnimationFrame(() => rightPanelCloseRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [compactLayout, rightPanelDrawerOpen]);

  function currentWorkspaceRoot() {
    return workspaceRootRef.current;
  }

  useEffect(() => {
    if (settingsOpen) {
      loadProviderProfiles();
      loadWorkerProfiles();
      loadMcpServers();
      loadSkills();
    }
  }, [settingsOpen]);

  const { status, lastError, request, reconnect } = useGatewayConnection({
    baseUrl: gatewayBase,
    resumeCursors,
    // 网关事件按 session_id 写入对应会话投影，支持多会话并发 run。
    onEvent: (event) => {
      const payload = event.payload || {};
      if (isRunTerminalEvent(payload) && payload.session_id) {
        // A6: Gateway OnRootRunTerminal mutates Goal (pause/fail/budget) before
        // Publish; re-hydrate so the Goal strip matches persisted state without
        // reopening the session. Live tool mutations still use goal_updated.
        void hydrateGoalsRef.current(payload.session_id);
      }
      let effects = [];
      setSessionRuntimes((map) => {
        const sessionId = resolveEventSessionId(payload, map, currentSessionIdRef.current);
        if (!sessionId) return map;
        const prev = map[sessionId] || createEmptySessionRuntime();
        const reduced = reduceRunEvent(prev, payload);
        effects = reduced.effects || [];
        return { ...map, [sessionId]: reduced.runtime };
      });
      for (const effect of effects) {
        if (effect.type === 'upsert_global_permission') {
          setGlobalPendingPermissions((items) => upsertByID(items, effect.permission));
          continue;
        }
        if (effect.type === 'clear_global_permissions_for_run') {
          setGlobalPendingPermissions((items) => items.filter((item) => item.runId !== effect.runId));
          continue;
        }
        if (effect.type === 'select_activity_tab') {
          setRightPanelTab('activity');
        }
      }
    },
  });

  useEffect(() => {
    if (status !== 'connected') return;
    const sessionId = currentSessionId;
    request('worker.list', currentRunId ? { run_id: currentRunId } : {}).then((result) => {
      const assignmentItems = Array.isArray(result?.assignments)
        ? result.assignments.map(normalizeAssignment)
        : [];
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        assignmentsById: Object.fromEntries(
          reconcileAssignmentsWithRuns(assignmentItems, rt.runs).map((item) => [item.id, item]),
        ),
        assignmentOrder: assignmentItems.map((item) => item.id),
      }));
    }).catch(() => {});
  }, [currentRunId, currentSessionId, patchRuntime, request, status]);

  const {
    hydrateTodos,
    hydrateGoals,
    continueGoal,
    cancelGoal,
  } = useGoalSession({
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
  });
  hydrateGoalsRef.current = hydrateGoals;

  const {
    createSession,
    deleteSession,
    deleteWorkspaceNode,
    browseWorkspaceDirectory,
    forkSession,
    compactSession,
    selectSession,
    sendTask,
    cancelRun,
  } = useSessionActions({
    dialog,
    sessions,
    setSessions,
    currentSessionId,
    setCurrentSessionId,
    workspace,
    setWorkspace,
    recentWorkspaces,
    setRecentWorkspaces,
    setSessionRuntimes,
    sessionRuntimes,
    sessionRuntimesRef,
    currentSessionIdRef,
    patchRuntime,
    setGlobalPendingPermissions,
    loadSessionState,
    loadGlobalPendingPermissions,
    openWorkspaceRoot,
    loadSkills,
    runSettings,
    request,
    hydrateGoals,
    continueGoal,
    cancelGoal,
    setRightPanelTab,
    contextTokenBudget,
    compacting,
  });

  async function cancelAssignment(assignment) {
    if (!assignment?.id || !assignment.runId || !['queued', 'running', 'waiting_permission'].includes(assignment.status)) {
      return;
    }
    patchCurrentRuntime((rt) => ({ ...rt, assignmentsById: {
      ...rt.assignmentsById,
      [assignment.id]: { ...assignment, status: 'cancelling', summary: '已请求取消' },
    } }));
    try {
      await request('worker.assignment.cancel', {
        run_id: assignment.runId,
        assignment_id: assignment.id,
      });
    } catch (error) {
      patchCurrentRuntime((rt) => ({ ...rt, assignmentsById: {
        ...rt.assignmentsById,
        [assignment.id]: { ...assignment, summary: error.message },
      } }));
      setMessages((items) => [
        ...items,
        {
          id: `assignment_cancel_error_${Date.now()}`,
          role: 'assistant',
          agent: 'system',
          text: `取消 Worker 任务失败：${error.message}`,
        },
      ]);
    }
  }

  async function resolvePermission(id, decision) {
    const item = permissions.find((permission) => permission.id === id) ||
      globalPendingPermissions.find((permission) => permission.id === id);
    setPermissions((items) => items.map((permission) => (
      permission.id === id
        ? { ...permission, status: 'resolved', decision }
        : permission
    )));
    setGlobalPendingPermissions((items) => items.filter((permission) => permission.id !== id));
    setRuns((items) => items.map((run) => (
      run.id === (item?.runId || currentRunId) && run.status === 'waiting_permission'
        ? { ...run, status: 'running', updatedAt: new Date().toISOString() }
        : run
    )));
    await request('permission.resolve', {
      permission_id: id,
      run_id: item?.runId || currentRunId,
      decision,
    }).catch(() => {});
  }

  async function loadRunEvents(runId) {
    if (!runId) {
      return [];
    }
    setRunEventsLoading((items) => ({ ...items, [runId]: true }));
    setRunEventsError((items) => ({ ...items, [runId]: '' }));
    try {
      const items = await apiJson(`/api/v1/runs/${encodeURIComponent(runId)}/events`);
      const normalized = Array.isArray(items) ? items.map(normalizeRunEvent) : [];
      setRunEventsByRun((current) => ({ ...current, [runId]: normalized }));
      return normalized;
    } catch (error) {
      setRunEventsError((items) => ({ ...items, [runId]: error.message }));
      return [];
    } finally {
      setRunEventsLoading((items) => ({ ...items, [runId]: false }));
    }
  }

  function selectRightPanelTab(tab) {
    setRightPanelTab(tab);
    setWorkspacePanelExpanded(tab === 'workspace' && !compactLayout);
    if (compactLayout) {
      if (!rightPanelDrawerOpen) rightPanelReturnFocusRef.current = document.activeElement;
      setRightPanelDrawerOpen(true);
    }
  }

  function closeRightPanelDrawer() {
    setRightPanelDrawerOpen(false);
    const returnTarget = rightPanelReturnFocusRef.current;
    window.requestAnimationFrame(() => {
      if (returnTarget && typeof returnTarget.focus === 'function' && document.contains(returnTarget)) {
        returnTarget.focus();
      }
    });
  }

  function handleRightPanelTabsKeyDown(event) {
    const currentIndex = rightPanelTabs.findIndex((tab) => tab.id === rightPanelTab);
    if (currentIndex < 0) {
      return;
    }

    const lastIndex = rightPanelTabs.length - 1;
    let nextIndex = currentIndex;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
      nextIndex = currentIndex === lastIndex ? 0 : currentIndex + 1;
    } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
      nextIndex = currentIndex === 0 ? lastIndex : currentIndex - 1;
    } else if (event.key === 'Home') {
      nextIndex = 0;
    } else if (event.key === 'End') {
      nextIndex = lastIndex;
    } else {
      return;
    }

    event.preventDefault();
    const tabList = event.currentTarget;
    const nextTab = rightPanelTabs[nextIndex];
    setRightPanelTab(nextTab.id);
    setWorkspacePanelExpanded(nextTab.id === 'workspace' && !compactLayout);
    window.requestAnimationFrame(() => {
      tabList.querySelector(`[data-right-panel-tab="${nextTab.id}"]`)?.focus();
    });
  }

  function openWorkerConversation(assignment) {
    if (!assignment?.id) return;
    const tabId = `worker:${assignment.id}`;
    const title = displayWorkerProfileName(
      assignment.profileKey || assignment.workerId || assignment.id,
    );
    patchCurrentRuntime((rt) => {
      const tabs = rt.conversationTabs || [{
        id: 'main', kind: 'main', title: '主对话', closable: false,
      }];
      const nextTab = {
        id: tabId,
        kind: 'worker',
        assignmentId: assignment.id,
        workerId: assignment.workerId || '',
        runId: assignment.runId || '',
        task: assignment.task || '',
        title,
        status: assignment.status,
        statusLabel: displayStatus(assignment.status),
        closable: true,
      };
      return {
        ...rt,
        conversationTabs: tabs.some((tab) => tab.id === tabId)
          ? tabs.map((tab) => (tab.id === tabId ? { ...tab, ...nextTab } : tab))
          : [...tabs, nextTab],
        activeConversationTab: tabId,
      };
    });
  }

  function closeConversationTab(tabId) {
    if (!tabId || tabId === 'main') return;
    patchCurrentRuntime((rt) => ({
      ...rt,
      conversationTabs: (rt.conversationTabs || []).filter((tab) => tab.id !== tabId),
      activeConversationTab: rt.activeConversationTab === tabId ? 'main' : rt.activeConversationTab,
    }));
  }

  const rightPanelContent = rightPanelTab === 'workspace' ? (
    <WorkspacePanel
      apiJson={apiJson}
      canFloat={!compactLayout}
      expanded={workspacePanelExpanded}
      onExpandedChange={setWorkspacePanelExpanded}
      workspace={workspace}
    />
  ) : rightPanelTab === 'workers' ? (
    <WorkerPanel
      assignments={assignments}
      onCancelAssignment={cancelAssignment}
      onOpenAssignment={openWorkerConversation}
    />
  ) : rightPanelTab === 'memory' ? (
    <MemoryPanel
      apiJson={apiJson}
      currentSessionId={currentSessionId}
      workspaceRoot={workspace?.root_path || workspace?.root || ''}
    />
  ) : (
    <RunActivityPanel
      currentRunId={currentRunId}
      globalPendingPermissions={globalPendingPermissions}
      onLoadRunEvents={loadRunEvents}
      onResolvePermission={resolvePermission}
      permissions={permissions}
      runEventsByRun={runEventsByRun}
      runEventsError={runEventsError}
      runEventsLoading={runEventsLoading}
      runs={runs}
      tools={tools}
    />
  );

  return (
    <div className="app-shell">
      <TopBar
        busy={activeRunCount > 0}
        gatewayBase={gatewayBase}
        onReconnect={reconnect}
        onSettings={() => setSettingsOpen(true)}
        status={status}
      />
      <SettingsPanel
        workerProfiles={workerProfiles}
        workerProfilesError={workerProfilesError}
        workerProfilesLoading={workerProfilesLoading}
        mcpServers={mcpServers}
        mcpServersError={mcpServersError}
        mcpServersLoading={mcpServersLoading}
        mcpDiscoveryByServer={mcpDiscoveryByServer}
        onCreateWorkerProfile={createWorkerProfile}
        onCreateMcpServer={createMcpServer}
        onCreateProviderProfile={createProviderProfile}
        onCreateSkill={createSkill}
        onDeleteWorkerProfile={deleteWorkerProfile}
        onDeleteMcpServer={deleteMcpServer}
        onDeleteProviderProfile={deleteProviderProfile}
        onDeleteSkill={deleteSkill}
        onDiscoverMcpServer={discoverMcpServer}
        onLoadSkillDetail={loadSkillDetail}
        onChange={setRunSettings}
        onClose={() => setSettingsOpen(false)}
        onRefreshWorkerProfiles={loadWorkerProfiles}
        onRefreshMcpServers={loadMcpServers}
        onRefreshProviderProfiles={loadProviderProfiles}
        onRefreshSkills={loadSkills}
        onUpdateWorkerProfile={updateWorkerProfile}
        onUpdateMcpServer={updateMcpServer}
        onUpdateProviderProfile={updateProviderProfile}
        onUpdateSkill={updateSkill}
        open={settingsOpen}
        providerProfiles={providerProfiles}
        providerProfilesError={providerProfilesError}
        providerProfilesLoading={providerProfilesLoading}
        settings={runSettings}
        skills={skills}
        skillsError={skillsError}
        skillsLoading={skillsLoading}
        workspaceRoot={currentWorkspaceRoot()}
      />
      <main
        className={[
          'workspace',
          rightPanelResizing ? 'is-resizing-right' : '',
          workspacePanelExpanded && !compactLayout ? 'workspace-panel-expanded' : '',
        ].filter(Boolean).join(' ')}
        style={compactLayout ? undefined : { '--right-panel-width': `${rightPanelWidth}px` }}
      >
        <Sidebar
          currentSessionId={currentSessionId}
          onCompactSession={compactSession}
          onDeleteSession={deleteSession}
          onDeleteWorkspace={deleteWorkspaceNode}
          onForkSession={forkSession}
          onNewSession={createSession}
          onOpenWorkspace={() => setWorkspacePickerOpen(true)}
          onSelectSession={selectSession}
          onSelectWorkspace={selectWorkspaceNode}
          sessionRunStatus={sessionRunStatus}
          sessions={sessions}
          workspace={workspace}
          workspaces={recentWorkspaces}
        />
        <WorkspacePickerDialog
          currentRoot={workspace?.root_path || workspace?.root || ''}
          onBrowse={browseWorkspaceDirectory}
          onClose={() => setWorkspacePickerOpen(false)}
          onOpen={openWorkspaceRoot}
          open={workspacePickerOpen}
          workspaces={recentWorkspaces}
        />
        <ChatPanel
          activeConversationTab={activeConversationTab}
          conversationTabs={displayedConversationTabs}
          draft={draft}
          messages={messages}
          onCancel={cancelRun}
          onCloseConversationTab={closeConversationTab}
          onDraftChange={setDraft}
          onProviderProfileChange={(id) => setRunSettings((current) => ({
            ...current,
            providerProfileId: id,
            model: '',
          }))}
          onResolvePermission={resolvePermission}
          onSend={sendTask}
          onSelectConversationTab={(tabId) => patchCurrentRuntime((rt) => ({
            ...rt, activeConversationTab: tabId,
          }))}
          onTodosExpandToggle={() => patchCurrentRuntime((rt) => ({
            ...rt,
            todosExpanded: !rt.todosExpanded,
          }))}
          onTodosRefresh={() => hydrateTodos(currentSessionId)}
          goal={goal}
          goalBusy={goalBusy}
          goalExpanded={goalExpanded}
          goalLoading={!goalHydrated && !goal}
          goalSessionId={currentSessionId}
          onGoalCancel={() => { cancelGoal().catch(() => {}); }}
          onGoalContinue={() => { continueGoal().catch(() => {}); }}
          onGoalExpandToggle={() => patchCurrentRuntime((rt) => ({
            ...rt,
            goalExpanded: !rt.goalExpanded,
          }))}
          permissions={pendingPermissions}
          providerProfileId={runSettings.providerProfileId}
          providerProfiles={providerProfiles}
          running={running}
          todoOpenCount={todoOpenCount}
          todos={todos}
          todosExpanded={todosExpanded}
          todosLoading={!todosHydrated && todos.length === 0}
          tokenBudgetEnabled={contextTokenBudget.enabled}
          tokenDisplayRatio={contextTokenBudget.displayRatio}
          tokenMax={contextTokenBudget.maxTokens}
          tokenRatio={contextTokenBudget.ratio}
          tokenSoftBudget={contextTokenBudget.softBudget}
          tokenUsed={contextTokenBudget.used}
          tools={tools}
        />
        {/* <div
          aria-label="辅助面板"
          className="right-panel-rail"
          onKeyDown={handleRightPanelTabsKeyDown}
          role="tablist"
        >
          {rightPanelTabs.map((tab) => (
            <TabButton
              active={rightPanelTab === tab.id}
              data-right-panel-tab={tab.id}
              data-testid={tab.testId ? `${tab.testId}-rail` : undefined}
              key={tab.id}
              onClick={() => selectRightPanelTab(tab.id)}
              panelId="right-panel-content"
            >
              {tab.label}
            </TabButton>
          ))}
        </div> */}
        {rightPanelDrawerOpen ? (
          <button
            aria-label="关闭辅助面板"
            aria-hidden="true"
            className="right-panel-backdrop"
            onClick={closeRightPanelDrawer}
            tabIndex={-1}
            type="button"
          />
        ) : null}
        <aside
          aria-hidden={compactLayout && !rightPanelDrawerOpen ? 'true' : undefined}
          className={[
            'right-panel',
            rightPanelDrawerOpen ? 'drawer-open' : '',
            workspacePanelExpanded && !compactLayout ? 'workspace-floating' : '',
          ].filter(Boolean).join(' ')}
          inert={compactLayout && !rightPanelDrawerOpen ? '' : undefined}
          onKeyDown={(event) => {
            if (compactLayout && event.key === 'Escape') closeRightPanelDrawer();
            if (!compactLayout && workspacePanelExpanded && event.key === 'Escape') {
              event.stopPropagation();
              setWorkspacePanelExpanded(false);
            }
          }}
        >
          {!compactLayout && !workspacePanelExpanded ? (
            <button
              aria-label="拖拽调整右侧面板宽度"
              aria-orientation="vertical"
              aria-valuemax={RIGHT_PANEL_WIDTH_MAX}
              aria-valuemin={RIGHT_PANEL_WIDTH_MIN}
              aria-valuenow={rightPanelWidth}
              className="right-panel-resizer"
              data-testid="right-panel-resizer"
              onDoubleClick={() => setRightPanelWidth(RIGHT_PANEL_WIDTH_DEFAULT)}
              onKeyDown={(event) => {
                if (event.key === 'ArrowLeft') {
                  event.preventDefault();
                  setRightPanelWidth((w) => clampRightPanelWidth(w + 16));
                } else if (event.key === 'ArrowRight') {
                  event.preventDefault();
                  setRightPanelWidth((w) => clampRightPanelWidth(w - 16));
                } else if (event.key === 'Home') {
                  event.preventDefault();
                  setRightPanelWidth(RIGHT_PANEL_WIDTH_MAX);
                } else if (event.key === 'End') {
                  event.preventDefault();
                  setRightPanelWidth(RIGHT_PANEL_WIDTH_MIN);
                }
              }}
              onPointerDown={startRightPanelResize}
              role="separator"
              type="button"
            />
          ) : null}
          <div className="right-panel-mobile-header">
            <strong>{rightPanelTabs.find((tab) => tab.id === rightPanelTab)?.label || '辅助面板'}</strong>
            <IconButton label="关闭辅助面板" onClick={closeRightPanelDrawer} ref={rightPanelCloseRef}>
              <X size={17} />
            </IconButton>
          </div>
          <div
            aria-label="辅助面板"
            className="right-panel-tabs"
            onKeyDown={handleRightPanelTabsKeyDown}
            role="tablist"
          >
            {rightPanelTabs.map((tab) => (
              <TabButton
                active={rightPanelTab === tab.id}
                data-right-panel-tab={tab.id}
                data-testid={tab.testId || undefined}
                key={tab.id}
                onClick={() => selectRightPanelTab(tab.id)}
                panelId="right-panel-content"
              >
                {tab.label}
              </TabButton>
            ))}
          </div>
          <div className="right-panel-content" id="right-panel-content" role="tabpanel">
            {rightPanelContent}
          </div>
        </aside>
      </main>
      {lastError ? <div className="toast" role="alert">{lastError}</div> : null}
      <StatusBar
        runSeq={runSeq}
        runtimeStatus={
          activeRunCount > 0
            ? `${activeRunCount} 个会话运行中（${displayRuntimeMode(runSettings.runtimeMode)}）`
            : `${displayRuntimeMode(runSettings.runtimeMode)} 待命`
        }
        status={status}
      />
    </div>
  );
}
