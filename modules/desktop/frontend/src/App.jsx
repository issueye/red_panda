import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { ChatPanel } from './components/chat/ChatPanel.jsx';
import { MemoryPanel } from './components/MemoryPanel.jsx';
import { RunActivityPanel } from './components/RunActivityPanel.jsx';
import { SchedulesDialog } from './components/SchedulesDialog.jsx';
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
import { useConversationTabs } from './hooks/useConversationTabs.js';
import { useGatewayConnection } from './hooks/useGatewayConnection.js';
import { useGatewayResources } from './hooks/useGatewayResources.js';
import { useGoalSession } from './hooks/useGoalSession.js';
import { usePermissionActions } from './hooks/usePermissionActions.js';
import { useResizablePanels } from './hooks/useResizablePanels.js';
import { useRightPanelChrome } from './hooks/useRightPanelChrome.js';
import { useRunActivityActions } from './hooks/useRunActivityActions.js';
import { useSessionActions } from './hooks/useSessionActions.js';
import {
  useSessionBootstrap,
} from './hooks/useSessionBootstrap.js';
import { apiJson, gatewayBase } from './lib/api.js';
import { normalizeAssignment } from './lib/assignments.js';
import {
  filterMainMessages,
  filterMainTools,
  filterVisibleMessagesAfterCompaction,
} from './lib/conversationScope.js';
import { displayRuntimeMode, displayStatus, displayWorkerProfileName } from './lib/displayLabels.js';
import {
  clampLeftPanelWidth,
  clampRightPanelWidth,
  LEFT_PANEL_WIDTH_DEFAULT,
  LEFT_PANEL_WIDTH_MAX,
  LEFT_PANEL_WIDTH_MIN,
  RIGHT_PANEL_WIDTH_DEFAULT,
  RIGHT_PANEL_WIDTH_MAX,
  RIGHT_PANEL_WIDTH_MIN,
  rightPanelTabs,
} from './lib/panelLayout.js';
import { providerModelFor } from './lib/providerProfiles.js';
import { isRunTerminalEvent } from './lib/runEventLifecycle.js';
import { loadRunSettings, persistRunSettings } from './lib/runSettingsStorage.js';
import { reconcileAssignmentsWithRuns, reduceRunEvent, upsertByID } from './lib/reduceRunEvent.js';
import {
  collectResumeCursors,
  collectSessionRunStatus,
  countActiveRuns,
  createEmptySessionRuntime,
  patchSessionRuntimeMap,
  pruneIdleSessionRuntimes,
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

export function App() {
  const dialog = useDialog();
  const [sessionRuntimes, setSessionRuntimes] = useState(() => ({}));
  const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false);
  const [leftPanelTab, setLeftPanelTab] = useState('sessions');
  const [workspacePanelExpanded, setWorkspacePanelExpanded] = useState(false);
  const [compactLayout, setCompactLayout] = useState(() => (
    typeof window !== 'undefined' && window.matchMedia('(max-width: 1100px)').matches
  ));
  const {
    leftPanelWidth,
    setLeftPanelWidth,
    leftPanelResizing,
    rightPanelWidth,
    setRightPanelWidth,
    rightPanelResizing,
    startLeftPanelResize,
    startRightPanelResize,
  } = useResizablePanels({ compactLayout });
  const {
    rightPanelTab,
    setRightPanelTab,
    rightPanelDrawerOpen,
    setRightPanelDrawerOpen,
    rightPanelCloseRef,
    selectRightPanelTab,
    closeRightPanelDrawer,
    handleRightPanelTabsKeyDown,
  } = useRightPanelChrome({ compactLayout });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [schedulesOpen, setSchedulesOpen] = useState(false);
  const [runSettings, setRunSettings] = useState(loadRunSettings);
  const workspaceRootRef = useRef('');
  const getWorkspaceRoot = useCallback(() => workspaceRootRef.current, []);
  const gatewayResources = useGatewayResources({
    getWorkspaceRoot,
    setRunSettings,
  });
  const { providers, workers, mcp, skills } = gatewayResources;
  const providerProfiles = providers.items;
  const loadProviderProfiles = providers.load;
  const loadWorkerProfiles = workers.load;
  const loadMcpServers = mcp.load;
  const loadSkills = skills.load;
  const currentSessionIdRef = useRef('');

  const patchRuntime = useCallback((sessionId, updater) => {
    if (!sessionId) return;
    setSessionRuntimes((map) => {
      const next = patchSessionRuntimeMap(map, sessionId, updater);
      // Bound idle session projections in memory (docs/48 Wave D).
      return pruneIdleSessionRuntimes(next, {
        keepSessionId: currentSessionIdRef.current || sessionId,
      });
    });
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

  const hydrateGoalsRef = useRef(async () => {});
  const upsertSessionRef = useRef(null);
  const selectSessionRef = useRef(null);
  const refreshSessionsRef = useRef(null);
  const gatewayRequestRef = useRef(null);
  currentSessionIdRef.current = currentSessionId;
  const sessionRuntimesRef = useRef(sessionRuntimes);
  sessionRuntimesRef.current = sessionRuntimes;

  const runtime = sessionRuntimes[currentSessionId] || createEmptySessionRuntime({
    messages: currentSessionId ? [] : initialMessages,
    hydrated: !currentSessionId,
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
    contextSummaryCoveredCount = 0,
    contextSummaryKeepTailTurns = 0,
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

  const visibleMessages = useMemo(() => filterVisibleMessagesAfterCompaction(messages, {
    endSeq: contextSummaryEndSeq,
    coveredCount: contextSummaryCoveredCount,
    keepTailTurns: contextSummaryKeepTailTurns,
  }), [
    messages,
    contextSummaryEndSeq,
    contextSummaryCoveredCount,
    contextSummaryKeepTailTurns,
  ]);
  const mainContextMessages = useMemo(() => filterMainMessages(messages), [messages]);
  const mainContextTools = useMemo(() => filterMainTools(tools), [tools]);

  const selectedProviderProfile = useMemo(() => {
    if (!runSettings.providerProfileId) {
      return providerProfiles.find((item) => item.isDefault && item.active !== false)
        || providerProfiles.find((item) => item.active !== false)
        || null;
    }
    return providerProfiles.find((item) => item.id === runSettings.providerProfileId) || null;
  }, [providerProfiles, runSettings.providerProfileId]);

  const contextTokenBudget = useMemo(() => {
    const used = estimateEffectiveSessionTokens(mainContextMessages, draft, mainContextTools, {
      endSeq: contextSummaryEndSeq,
      summary: contextSummary,
      coveredCount: contextSummaryCoveredCount,
      keepTailTurns: contextSummaryKeepTailTurns,
    });
    const selectedModel = providerModelFor(selectedProviderProfile, runSettings.model);
    const maxTokens = Number(selectedModel?.maxTokens || selectedProviderProfile?.maxTokens) || 0;
    return tokenBudgetState(used, maxTokens);
  }, [
    mainContextMessages,
    draft,
    mainContextTools,
    selectedProviderProfile,
    runSettings.model,
    contextSummary,
    contextSummaryEndSeq,
    contextSummaryCoveredCount,
    contextSummaryKeepTailTurns,
  ]);

  useEffect(() => {
    persistRunSettings(runSettings);
  }, [runSettings]);

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
      // Out-of-band session mutations (create / schedule / delete).
      if (event?.method === 'session.upserted') {
        const sessionPayload = event.payload?.session || event.payload;
        const runId = event.payload?.run_id || '';
        const upserted = upsertSessionRef.current?.(sessionPayload);
        const sessionId = upserted?.id || sessionPayload?.id || '';
        if (sessionId) {
          // Auto-open schedule-created sessions so the left tree + chat stay in sync.
          if (event.payload?.source === 'schedule' || event.payload?.reason === 'schedule') {
            void selectSessionRef.current?.(sessionId);
          }
          if (runId) {
            gatewayRequestRef.current?.('run.subscribe', {
              runs: [{ run_id: runId, after_seq: 0 }],
            }).catch(() => {});
          }
        } else {
          void refreshSessionsRef.current?.();
        }
        return;
      }
      if (event?.method === 'session.deleted') {
        const deletedId = event.payload?.id || '';
        if (!deletedId) return;
        setSessions((items) => items.filter((item) => item.id !== deletedId));
        setSessionRuntimes((map) => {
          if (!map[deletedId]) return map;
          const next = { ...map };
          delete next[deletedId];
          return next;
        });
        if (currentSessionIdRef.current === deletedId) {
          void refreshSessionsRef.current?.().then((list) => {
            const nextId = Array.isArray(list) && list[0]?.id ? list[0].id : '';
            if (nextId) void selectSessionRef.current?.(nextId);
            else setCurrentSessionId('');
          }).catch(() => setCurrentSessionId(''));
        }
        return;
      }

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
  gatewayRequestRef.current = request;

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
    selectSession,
    sendTask,
    cancelRun,
    upsertSession,
    refreshSessions,
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
  upsertSessionRef.current = upsertSession;
  selectSessionRef.current = selectSession;
  refreshSessionsRef.current = refreshSessions;

  const { resolvePermission } = usePermissionActions({
    permissions,
    globalPendingPermissions,
    currentRunId,
    setPermissions,
    setGlobalPendingPermissions,
    setRuns,
    request,
  });
  const { cancelAssignment, loadRunEvents } = useRunActivityActions({
    patchCurrentRuntime,
    setMessages,
    setRunEventsByRun,
    setRunEventsLoading,
    setRunEventsError,
    request,
  });
  const { openWorkerConversation, closeConversationTab } = useConversationTabs({
    patchCurrentRuntime,
  });

  function selectLeftPanelTab(tab) {
    setLeftPanelTab(tab);
    if (tab !== 'workspace') setWorkspacePanelExpanded(false);
  }

  const workspacePanel = (
    <WorkspacePanel
      apiJson={apiJson}
      canFloat={!compactLayout}
      expanded={workspacePanelExpanded}
      onExpandedChange={setWorkspacePanelExpanded}
      workspace={workspace}
    />
  );

  const rightPanelContent = rightPanelTab === 'workers' ? (
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
        onSchedules={() => setSchedulesOpen(true)}
        onSettings={() => setSettingsOpen(true)}
        status={status}
      />
      <SettingsPanel
        onChange={setRunSettings}
        onClose={() => setSettingsOpen(false)}
        providers={providers}
        workers={workers}
        mcp={mcp}
        skills={skills}
        open={settingsOpen}
        settings={runSettings}
        workspaceRoot={currentWorkspaceRoot()}
      />
      <SchedulesDialog
        apiJson={apiJson}
        onClose={() => setSchedulesOpen(false)}
        onOpenSession={selectSession}
        open={schedulesOpen}
        providerProfileId={runSettings?.providerProfileId || ''}
        workspaceRoot={workspace?.root_path || workspace?.root || currentWorkspaceRoot?.() || ''}
        workspaces={recentWorkspaces}
      />
      <main
        className={[
          'workspace',
          leftPanelResizing ? 'is-resizing-left' : '',
          rightPanelResizing ? 'is-resizing-right' : '',
        ].filter(Boolean).join(' ')}
        style={compactLayout ? undefined : {
          '--left-panel-width': `${leftPanelWidth}px`,
          '--right-panel-width': `${rightPanelWidth}px`,
        }}
      >
        <Sidebar
          currentSessionId={currentSessionId}
          leftTab={leftPanelTab}
          onDeleteSession={deleteSession}
          onDeleteWorkspace={deleteWorkspaceNode}
          onLeftTabChange={selectLeftPanelTab}
          onNewSession={createSession}
          onOpenWorkspace={() => setWorkspacePickerOpen(true)}
          onSelectSession={selectSession}
          onSelectWorkspace={selectWorkspaceNode}
          onWorkspaceExpandedKeyDown={(event) => {
            if (!workspacePanelExpanded) return;
            if (event.key === 'Escape') {
              event.stopPropagation();
              setWorkspacePanelExpanded(false);
            }
          }}
          sessionRunStatus={sessionRunStatus}
          sessions={sessions}
          workspace={workspace}
          workspaceExpanded={workspacePanelExpanded}
          workspacePanel={workspacePanel}
          workspaces={recentWorkspaces}
        />
        {!compactLayout ? (
          <button
            aria-label="拖拽调整左侧面板宽度"
            aria-orientation="vertical"
            aria-valuemax={LEFT_PANEL_WIDTH_MAX}
            aria-valuemin={LEFT_PANEL_WIDTH_MIN}
            aria-valuenow={leftPanelWidth}
            className="left-panel-resizer"
            data-testid="left-panel-resizer"
            onDoubleClick={() => setLeftPanelWidth(LEFT_PANEL_WIDTH_DEFAULT)}
            onKeyDown={(event) => {
              if (event.key === 'ArrowLeft') {
                event.preventDefault();
                setLeftPanelWidth((width) => clampLeftPanelWidth(width - 16));
              } else if (event.key === 'ArrowRight') {
                event.preventDefault();
                setLeftPanelWidth((width) => clampLeftPanelWidth(width + 16));
              } else if (event.key === 'Home') {
                event.preventDefault();
                setLeftPanelWidth(LEFT_PANEL_WIDTH_MIN);
              } else if (event.key === 'End') {
                event.preventDefault();
                setLeftPanelWidth(LEFT_PANEL_WIDTH_MAX);
              }
            }}
            onPointerDown={startLeftPanelResize}
            role="separator"
            type="button"
          />
        ) : null}
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
          messages={visibleMessages}
          onCancel={cancelRun}
          onCloseConversationTab={closeConversationTab}
          onDraftChange={setDraft}
          onProviderProfileChange={(id, model) => setRunSettings((current) => ({
            ...current,
            providerProfileId: id,
            model,
            reasoningEffort: '',
          }))}
          onReasoningEffortChange={(reasoningEffort) => setRunSettings((current) => ({
            ...current,
            reasoningEffort,
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
          model={runSettings.model}
          reasoningEffort={runSettings.reasoningEffort}
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
          workspaceRoot={currentWorkspaceRoot()}
        />
        <div
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
        </div>
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
        {workspacePanelExpanded && !compactLayout && leftPanelTab === 'workspace' ? (
          <button
            aria-label="返回侧栏"
            className="workspace-window-backdrop"
            onClick={() => setWorkspacePanelExpanded(false)}
            tabIndex={-1}
            type="button"
          />
        ) : null}
        <aside
          aria-label="辅助面板"
          aria-hidden={compactLayout && !rightPanelDrawerOpen ? 'true' : undefined}
          className={[
            'right-panel',
            rightPanelDrawerOpen ? 'drawer-open' : '',
          ].filter(Boolean).join(' ')}
          inert={compactLayout && !rightPanelDrawerOpen ? '' : undefined}
          onKeyDown={(event) => {
            if (compactLayout && event.key === 'Escape') closeRightPanelDrawer();
          }}
        >
          {!compactLayout ? (
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
          <div
            className="right-panel-content"
            id="right-panel-content"
            role="tabpanel"
          >
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
        toolCount={tools.length}
      />
    </div>
  );
}
