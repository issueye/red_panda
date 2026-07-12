import { useEffect, useMemo, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { ChatPanel } from './components/chat/ChatPanel.jsx';
import { MemoryPanel } from './components/MemoryPanel.jsx';
import { RunActivityPanel } from './components/RunActivityPanel.jsx';
import { SettingsPanel } from './components/SettingsPanel.jsx';
import { Sidebar } from './components/Sidebar.jsx';
import { StatusBar } from './components/StatusBar.jsx';
import { SubAgentPanel } from './components/SubAgentPanel.jsx';
import { TopBar } from './components/TopBar.jsx';
import { IconButton } from './components/ui/button.jsx';
import { useDialog } from './components/ui/dialog.jsx';
import { TabButton } from './components/ui/tabs.jsx';
import { WorkspacePanel } from './components/WorkspacePanel.jsx';
import { WorkspacePickerDialog } from './components/WorkspacePickerDialog.jsx';
import { useGatewayConnection } from './hooks/useGatewayConnection.js';
import { normalizeRunEvent } from './lib/activityEvents.js';
import { normalizeAgentList } from './lib/agents.js';
import { gatewayBaseURL } from './lib/config.js';
import {
  goalFromUpdatedEvent,
  goalShouldAutoContinue,
  normalizeGoalList,
  pickFocusGoal,
} from './lib/goals.js';
import { extractAgentScope } from './lib/conversationScope.js';
import { selectDirectory } from './lib/desktopShell.js';
import { displayRuntimeMode, displaySessionKind, displayStatus } from './lib/displayLabels.js';
import {
  mcpServerCreatePayload,
  mcpServerUpdatePayload,
  normalizeMcpDiscovery,
  normalizeMcpServer,
} from './lib/mcpServers.js';
import {
  normalizeProviderProfile,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
} from './lib/providerProfiles.js';
import { appendDiagnosticLog } from './lib/diagnosticLog.js';
import { parseCommand } from './lib/commands.js';
import { buildRunStartOptions, defaultRunSettings } from './lib/runOptions.js';
import { isRootTerminalRunEvent } from './lib/runEventLifecycle.js';
import {
  collectResumeCursors,
  collectSessionRunStatus,
  countActiveRuns,
  createEmptySessionRuntime,
  MAIN_CONVERSATION_TAB,
  patchSessionRuntimeMap,
  resolveEventSessionId,
} from './lib/sessionRuntime.js';
import {
  normalizeSkillDetail,
  normalizeSkillsList,
  skillCreatePayload,
  skillUpdatePayload,
} from './lib/skills.js';
import { resolveSubAgentLifecycleStatus } from './lib/subagentStatus.js';
import {
  countOpenTodos,
  normalizeTodo,
  todosFromToolFinishedPayload,
  todosFromUpdatedEvent,
} from './lib/todos.js';
import {
  estimateEffectiveSessionTokens,
  tokenBudgetState,
} from './lib/tokenBudget.js';

const gatewayBase = gatewayBaseURL();

const initialSessions = [
  { id: 'local-design', title: '架构', subtitle: '本地任务与子代理' },
];

const initialMessages = [
  {
    id: 'm1',
    role: 'assistant',
    agent: 'root',
    rootSeq: 1,
    text: '已连接。请输入任务。',
  },
];

const rightPanelTabs = [
  { id: 'workspace', label: '工作区', testId: '' },
  { id: 'subagents', label: '子代理', testId: '' },
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
    return { ...defaultRunSettings, ...JSON.parse(saved) };
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

async function apiJson(path, options = {}) {
  const response = await fetch(`${gatewayBase}${path}`, {
    headers: { 'content-type': 'application/json', ...(options.headers || {}) },
    ...options,
  });
  const body = await response.json();
  if (!response.ok || body.ok === false) {
    throw new Error(body.error?.message || `HTTP ${response.status}`);
  }
  return body.data;
}

function normalizeSession(session) {
  const kind = session.kind && session.kind !== 'normal' ? session.kind : '';
  const workspaceRoot = session.workspace_root || session.working_dir || '';
  const detail = workspaceRoot || displayStatus(session.status || 'active');
  const kindLabel = displaySessionKind(kind);
  return {
    id: session.id,
    title: session.title || session.name || session.id,
    subtitle: kind ? `${kindLabel || kind} - ${detail}` : detail,
    kind,
    parentId: session.parent_id || '',
    workspaceRoot,
  };
}

function normalizeWorkspace(item) {
  if (!item) return null;
  return {
    id: item.id,
    root: item.root || item.root_path || '',
    root_path: item.root_path || item.root || '',
    name: item.name || '',
    lastOpenedAt: item.last_opened_at,
  };
}

function normalizeHistoryMessage(message) {
  const firstText = Array.isArray(message.content)
    ? message.content.find((item) => item.type === 'text')?.text
    : '';
  return {
    id: message.id,
    role: message.role === 'user' ? 'user' : 'assistant',
    agent: message.role === 'subagent' ? 'subagent' : message.role,
    messageSeq: message.seq || 0,
    runId: message.run_id || '',
    createdAt: message.created_at,
    text: firstText || '',
  };
}

function normalizeToolCall(item) {
  return {
    id: item.id,
    rootRunId: item.root_run_id,
    name: item.tool_name || 'tool',
    displayName: item.display_name || item.tool_name || '工具',
    risk: item.risk || 'low',
    arguments: item.arguments || {},
    status: item.status || 'running',
    output: item.output || '',
    error: item.error || '',
    durationMs: item.duration_ms,
    startedSeq: item.started_seq || 0,
    rootSeq: item.finished_seq || item.started_seq || 0,
    startedAt: item.started_at,
  };
}

function normalizeRun(item) {
  return {
    id: item.id,
    sessionId: item.session_id,
    workspaceRoot: item.workspace_root || '',
    runtimeMode: item.runtime_mode || 'single_core',
    status: item.status || 'unknown',
    input: item.input || '',
    lastEventType: item.last_event_type || '',
    lastRootSeq: item.last_root_seq || 0,
    messageCount: item.message_count || 0,
    toolCount: item.tool_count || 0,
    error: item.error || '',
    startedAt: item.started_at,
    finishedAt: item.finished_at,
    updatedAt: item.updated_at,
  };
}

function normalizePermission(item) {
  return {
    id: item.id,
    runId: item.run_id,
    status: item.status || 'pending',
    decision: item.decision || '',
    summary: item.summary || '需要授权',
    detail: item.detail || item.tool_name || '系统正在等待处理决定。',
    risk: item.risk,
    toolName: item.tool_name,
    arguments: item.arguments || {},
    rootSeq: item.root_seq || 0,
    createdAt: item.created_at,
  };
}

function appendAgentText(items, payload, text) {
  const scope = extractAgentScope(payload);
  const agentName = scope.agentName || 'agent';
  const runId = payload.run_id || payload.root_run_id || '';
  const eventSeq = Number(payload.root_seq) || 0;
  const previous = items[items.length - 1];
  const canAppend = payload.type === 'message_delta'
    && previous?.role === 'assistant'
    && previous.agent === agentName
    && previous.runId === runId
    && (previous.subagentId || '') === (scope.subagentId || '')
    && previous.eventSeq > 0
    && eventSeq === previous.eventSeq + 1;

  if (canAppend) {
    return [
      ...items.slice(0, -1),
      {
        ...previous,
        eventSeq,
        rootSeq: eventSeq,
        text: `${previous.text}${text}`,
      },
    ];
  }

  return [
    ...items,
    {
      id: payload.event_id || payload.id || `evt_${Date.now()}`,
      role: 'assistant',
      agent: agentName,
      agentRole: scope.agentRole || '',
      subagentId: scope.subagentId || '',
      runId,
      eventSeq,
      rootSeq: eventSeq,
      createdAt: payload.created_at || new Date().toISOString(),
      text,
    },
  ];
}

function subagentConversationTabId(subagentId) {
  return `sub:${subagentId}`;
}

function latestActiveRun(runs) {
  if (!Array.isArray(runs)) return null;
  return runs
    .filter((item) => item.status === 'running' || item.status === 'waiting_permission')
    .sort((a, b) => new Date(b.updated_at || b.started_at || 0) - new Date(a.updated_at || a.started_at || 0))[0] || null;
}

function latestRootSeq(runs, fallback) {
  if (!Array.isArray(runs)) return fallback;
  return runs.reduce((max, item) => Math.max(max, item.last_root_seq || 0), fallback);
}

function countToolsForRun(items, runId) {
  if (!Array.isArray(items) || !runId) return 0;
  return items.filter((item) => item.rootRunId === runId).length;
}

function updateRunByID(items, runId, patch) {
  let found = false;
  const next = items.map((item) => {
    if (item.id !== runId) return item;
    found = true;
    return { ...item, ...patch };
  });
  return found ? next : items;
}

function upsertByID(items, nextItem) {
  return [
    ...items.filter((item) => item.id !== nextItem.id),
    nextItem,
  ];
}

export function App() {
  const dialog = useDialog();
  const [sessions, setSessions] = useState(initialSessions);
  const [currentSessionId, setCurrentSessionId] = useState('local-design');
  const [sessionRuntimes, setSessionRuntimes] = useState(() => ({
    'local-design': createEmptySessionRuntime({
      messages: initialMessages,
      hydrated: true,
    }),
  }));
  const [workspace, setWorkspace] = useState(null);
  const [recentWorkspaces, setRecentWorkspaces] = useState([]);
  const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false);
  const [globalPendingPermissions, setGlobalPendingPermissions] = useState([]);
  const [rightPanelTab, setRightPanelTab] = useState('workspace');
  const [rightPanelDrawerOpen, setRightPanelDrawerOpen] = useState(false);
  const [rightPanelWidth, setRightPanelWidth] = useState(loadRightPanelWidth);
  const [rightPanelResizing, setRightPanelResizing] = useState(false);
  const [compactLayout, setCompactLayout] = useState(() => (
    typeof window !== 'undefined' && window.matchMedia('(max-width: 1100px)').matches
  ));
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [runSettings, setRunSettings] = useState(loadRunSettings);
  const [providerProfiles, setProviderProfiles] = useState([]);
  const [providerProfilesLoading, setProviderProfilesLoading] = useState(false);
  const [providerProfilesError, setProviderProfilesError] = useState('');
  const [managedAgents, setManagedAgents] = useState([]);
  const [managedAgentsLoading, setManagedAgentsLoading] = useState(false);
  const [managedAgentsError, setManagedAgentsError] = useState('');
  const [mcpServers, setMcpServers] = useState([]);
  const [mcpServersLoading, setMcpServersLoading] = useState(false);
  const [mcpServersError, setMcpServersError] = useState('');
  const [mcpDiscoveryByServer, setMcpDiscoveryByServer] = useState({});
  const [skills, setSkills] = useState([]);
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [skillsError, setSkillsError] = useState('');
  const rightPanelCloseRef = useRef(null);
  const rightPanelReturnFocusRef = useRef(null);
  const rightPanelResizeRef = useRef(null);
  const autoCompactBusyRef = useRef(false);
  const autoCompactSessionRef = useRef('');
  const autoContinuedGoalsRef = useRef(new Set());
  const currentSessionIdRef = useRef(currentSessionId);
  currentSessionIdRef.current = currentSessionId;
  const sessionRuntimesRef = useRef(sessionRuntimes);
  sessionRuntimesRef.current = sessionRuntimes;

  const runtime = sessionRuntimes[currentSessionId] || createEmptySessionRuntime({
    messages: currentSessionId === 'local-design' ? initialMessages : [],
  });
  const {
    messages,
    tools,
    permissions,
    runs,
    subAgents,
    conversationTabs,
    activeConversationTab,
    running,
    currentRunId,
    rootSeq,
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
  } = runtime;

  function patchRuntime(sessionId, updater) {
    if (!sessionId) return;
    setSessionRuntimes((map) => patchSessionRuntimeMap(map, sessionId, updater));
  }

  function patchCurrentRuntime(updater) {
    patchRuntime(currentSessionIdRef.current, updater);
  }

  // UI interactions always target the focused session.
  const setMessages = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    messages: typeof value === 'function' ? value(rt.messages) : value,
  }));
  const setTools = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    tools: typeof value === 'function' ? value(rt.tools) : value,
  }));
  const setPermissions = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    permissions: typeof value === 'function' ? value(rt.permissions) : value,
  }));
  const setRuns = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    runs: typeof value === 'function' ? value(rt.runs) : value,
  }));
  const setSubAgents = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    subAgents: typeof value === 'function' ? value(rt.subAgents) : value,
  }));
  const setConversationTabs = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    conversationTabs: typeof value === 'function' ? value(rt.conversationTabs) : value,
  }));
  const setActiveConversationTab = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    activeConversationTab: typeof value === 'function' ? value(rt.activeConversationTab) : value,
  }));
  const setRunning = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    running: typeof value === 'function' ? value(rt.running) : value,
  }));
  const setCurrentRunId = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    currentRunId: typeof value === 'function' ? value(rt.currentRunId) : value,
  }));
  const setRootSeq = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    rootSeq: typeof value === 'function' ? value(rt.rootSeq) : value,
  }));
  const setRunEventsByRun = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    runEventsByRun: typeof value === 'function' ? value(rt.runEventsByRun) : value,
  }));
  const setRunEventsLoading = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    runEventsLoading: typeof value === 'function' ? value(rt.runEventsLoading) : value,
  }));
  const setRunEventsError = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    runEventsError: typeof value === 'function' ? value(rt.runEventsError) : value,
  }));
  const setDraft = (value) => patchCurrentRuntime((rt) => ({
    ...rt,
    draft: typeof value === 'function' ? value(rt.draft) : value,
  }));

  const agents = useMemo(() => [
    { id: 'root', name: 'root', role: 'root', status: running ? 'running' : 'idle', seq: rootSeq },
    ...subAgents,
  ], [rootSeq, running, subAgents]);
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

  async function loadGlobalPendingPermissions() {
    const items = await apiJson('/api/v1/permissions/pending').catch(() => []);
    setGlobalPendingPermissions(Array.isArray(items) ? items.map(normalizePermission) : []);
  }

  async function loadProviderProfiles() {
    setProviderProfilesLoading(true);
    setProviderProfilesError('');
    try {
      const items = await apiJson('/api/v1/provider-profiles');
      const normalized = Array.isArray(items) ? items.map(normalizeProviderProfile) : [];
      setProviderProfiles(normalized);
      setRunSettings((current) => {
        if (!current.providerProfileId || normalized.some((item) => item.id === current.providerProfileId)) {
          return current;
        }
        return { ...current, providerProfileId: '' };
      });
      return normalized;
    } catch (error) {
      setProviderProfilesError(error.message);
      return [];
    } finally {
      setProviderProfilesLoading(false);
    }
  }

  async function loadAgents() {
    setManagedAgentsLoading(true);
    setManagedAgentsError('');
    try {
      const data = await apiJson('/api/v1/agents');
      const normalized = normalizeAgentList(data);
      setManagedAgents(normalized);
      return normalized;
    } catch (error) {
      setManagedAgentsError(error.message);
      return [];
    } finally {
      setManagedAgentsLoading(false);
    }
  }

  async function createAgent(payload) {
    const created = await apiJson('/api/v1/agents', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    await loadAgents();
    return created;
  }

  async function updateAgent(id, payload) {
    const updated = await apiJson(`/api/v1/agents/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
    await loadAgents();
    return updated;
  }

  async function deleteAgent(id) {
    await apiJson(`/api/v1/agents/${encodeURIComponent(id)}`, { method: 'DELETE' });
    await loadAgents();
  }

  async function loadMcpServers() {
    setMcpServersLoading(true);
    setMcpServersError('');
    try {
      const data = await apiJson('/api/v1/mcp/servers');
      const normalized = Array.isArray(data?.servers)
        ? data.servers.map(normalizeMcpServer)
        : [];
      setMcpServers(normalized);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      return [];
    } finally {
      setMcpServersLoading(false);
    }
  }

  function currentWorkspaceRoot() {
    return workspace?.root_path || workspace?.root || '';
  }

  async function loadSkills(workspaceRootOverride) {
    const root = workspaceRootOverride || currentWorkspaceRoot();
    setSkillsLoading(true);
    setSkillsError('');
    if (!root) {
      setSkills([]);
      setSkillsLoading(false);
      setSkillsError('打开工作区后可管理托管技能。');
      return [];
    }
    try {
      const data = await apiJson(`/api/v1/skills?workspace_root=${encodeURIComponent(root)}`);
      const normalized = normalizeSkillsList(data);
      setSkills(normalized);
      return normalized;
    } catch (error) {
      setSkillsError(error.message);
      return [];
    } finally {
      setSkillsLoading(false);
    }
  }

  async function loadSkillDetail(name) {
    const root = currentWorkspaceRoot();
    if (!root || !name) {
      throw new Error('workspace and skill name are required');
    }
    const data = await apiJson(
      `/api/v1/skills/${encodeURIComponent(name)}?workspace_root=${encodeURIComponent(root)}&include_instructions=1`,
    );
    return normalizeSkillDetail(data);
  }

  async function hydrateTodos(sessionId) {
    if (!sessionId) return;
    try {
      const data = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/todos`);
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

  async function hydrateGoals(sessionId) {
    if (!sessionId || sessionId === 'local-design') return;
    try {
      const data = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/goals`);
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

  async function continueGoal(extraText = '', optionsOverrides = {}) {
    const sessionId = currentSessionIdRef.current;
    const current = sessionRuntimesRef.current[sessionId]?.goal;
    if (!sessionId || !current?.id) {
      throw new Error('当前没有可继续的目标');
    }
    patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: true }));
    try {
      const result = await apiJson(
        `/api/v1/sessions/${encodeURIComponent(sessionId)}/goals/${encodeURIComponent(current.id)}/continue`,
        {
          method: 'POST',
          body: JSON.stringify({
            input: extraText || '',
            options: buildRunStartOptions(runSettings, workspace, '', optionsOverrides),
          }),
        },
      );
      const nextRunId = result?.run_id || '';
      appendDiagnosticLog('info', `已继续目标 ${current.id}`, {
        source: 'goal',
        detail: { sessionId, goalId: current.id, runId: nextRunId },
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

  useEffect(() => {
    if (!currentSessionId || currentSessionId === 'local-design') return;
    if (!goalShouldAutoContinue(goal) || running || compacting || goalBusy) return;

    const key = `${currentSessionId}:${goal.id}:${goal.updatedAt || goal.usedToolTurns}`;
    if (autoContinuedGoalsRef.current.has(key)) return;
    autoContinuedGoalsRef.current.add(key);
    appendDiagnosticLog('info', `Goal 自动续跑 ${goal.id}`, {
      source: 'goal',
      detail: { sessionId: currentSessionId, goalId: goal.id, reason: goal.pauseReason },
    });
    continueGoal('', { goals_enabled: true }).catch(() => {
      // The key prevents a render loop. A later server update gets a new key.
    });
  }, [compacting, currentSessionId, goal, goalBusy, running]);

  async function cancelGoal() {
    const sessionId = currentSessionIdRef.current;
    const current = sessionRuntimesRef.current[sessionId]?.goal;
    if (!sessionId || !current?.id) {
      throw new Error('当前没有可取消的目标');
    }
    patchRuntime(sessionId, (prev) => ({ ...prev, goalBusy: true }));
    try {
      // If a run is active for this session, cancel it first (also pauses goal server-side).
      const runId = sessionRuntimesRef.current[sessionId]?.currentRunId;
      if (runId) {
        await request('run.cancel', { run_id: runId, reason: 'goal cancelled' }).catch(() => null);
      }
      await apiJson(
        `/api/v1/sessions/${encodeURIComponent(sessionId)}/goals/${encodeURIComponent(current.id)}/cancel`,
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

  async function loadSessionState(sessionId, { preserveLive = true } = {}) {
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
          return {
            ...prev,
            runs: normalizedRuns.length > 0 ? normalizedRuns : prev.runs,
            todos: todoData ? todoItems : prev.todos,
            todoOpenCount: todoData ? todoOpen : prev.todoOpenCount,
            todosHydrated: true,
            goals: goalData ? goalItems : prev.goals,
            goal: goalData ? focusGoal : prev.goal,
            goalHydrated: true,
            contextSummary: compactSummary,
            contextSummaryEndSeq: compactEndSeq,
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
          rootSeq: latestRootSeq(serverRuns, prev.rootSeq || 1),
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
          hydrated: true,
        };
      });
      loadGlobalPendingPermissions();
    } catch {
      patchRuntime(sessionId, () => createEmptySessionRuntime({ hydrated: true }));
      setGlobalPendingPermissions([]);
    }
  }

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
        const nextSessions = Array.isArray(data.sessions) ? data.sessions.map(normalizeSession) : [];
        if (nextSessions.length > 0) {
          setSessions(nextSessions);
          setCurrentSessionId(nextSessions[0].id);
          loadSessionState(nextSessions[0].id);
        } else {
          loadGlobalPendingPermissions();
        }
        loadProviderProfiles();
        loadAgents();
        loadMcpServers();
        loadSkills(nextWorkspace?.root_path || nextWorkspace?.root || '');
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (settingsOpen) {
      loadProviderProfiles();
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
      if (isRootTerminalRunEvent(payload) && payload.session_id) {
        // Gateway applies Goal terminal/pause hooks before publishing the event.
        // Rehydrate so Gateway-owned transitions (natural pause, compact, cancel)
        // cannot leave the Goal strip showing stale active state.
        void hydrateGoals(payload.session_id);
      }
      setSessionRuntimes((map) => {
        const sessionId = resolveEventSessionId(payload, map, currentSessionIdRef.current);
        if (!sessionId) return map;
        const prev = map[sessionId] || createEmptySessionRuntime();
        let next = {
          ...prev,
          rootSeq: Math.max(prev.rootSeq || 1, payload.root_seq || 0),
          running: prev.running || Boolean(payload.root_run_id),
          currentRunId: prev.currentRunId || payload.root_run_id || '',
          hydrated: true,
        };

        const agent = payload.agent || {};
        if (agent.role === 'subagent' || payload.type === 'subagent_update') {
          const id = agent.subagent_id || payload.payload?.subagent_id || agent.agent_id;
          if (id) {
            const current = next.subAgents.find((item) => item.id === id);
            const nextStatus = resolveSubAgentLifecycleStatus(
              payload.type,
              payload.payload,
              current?.status,
            );
            const nextSummary = payload.type === 'subagent_update'
              ? (payload.payload?.summary || current?.summary || '')
              : (current?.summary || payload.payload?.summary || '');
            const sub = {
              id,
              role: 'subagent',
              name: payload.payload?.name || agent.name || current?.name || id,
              status: nextStatus,
              backend: payload.payload?.backend || current?.backend || 'in_process',
              rootRunId: payload.root_run_id || current?.rootRunId || '',
              runId: payload.run_id || current?.runId || '',
              parentRunId: payload.parent_run_id || current?.parentRunId || '',
              summary: nextSummary,
              seq: payload.agent_seq || current?.seq || 0,
            };
            next = {
              ...next,
              subAgents: current
                ? next.subAgents.map((item) => (item.id === id ? sub : item))
                : [...next.subAgents, sub],
              conversationTabs: next.conversationTabs.map((tab) => (
                tab.subagentId === id
                  ? {
                      ...tab,
                      title: sub.name || tab.title,
                      status: sub.status,
                      statusLabel: displayStatus(sub.status),
                      runId: sub.runId || tab.runId,
                    }
                  : tab
              )),
            };
          }
        }

        if (payload.type === 'permission_required') {
          const permissionID = payload.payload?.permission_id || `perm_${Date.now()}`;
          const scope = extractAgentScope(payload);
          const nextPermission = {
            id: permissionID,
            runId: payload.payload?.run_id || payload.root_run_id,
            sessionId: payload.session_id || sessionId,
            status: 'pending',
            summary: payload.payload?.summary || '需要授权',
            detail: payload.payload?.detail || payload.payload?.tool_name || '系统正在等待处理决定。',
            risk: payload.payload?.risk,
            toolName: payload.payload?.tool_name,
            arguments: payload.payload?.arguments || {},
            rootSeq: payload.root_seq,
            agent: scope.agentName,
            agentRole: scope.agentRole,
            subagentId: scope.subagentId,
            createdAt: payload.created_at || new Date().toISOString(),
          };
          next = {
            ...next,
            permissions: upsertByID(next.permissions, nextPermission),
            runs: next.runs.map((item) => (
              item.id === (payload.payload?.run_id || payload.root_run_id)
                ? {
                    ...item,
                    status: 'waiting_permission',
                    lastEventType: payload.type,
                    lastRootSeq: payload.root_seq,
                    updatedAt: new Date().toISOString(),
                  }
                : item
            )),
          };
          setGlobalPendingPermissions((items) => upsertByID(items, nextPermission));
          setRightPanelTab('activity');
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'tool_started') {
          const toolID = payload.payload?.tool_call_id || payload.event_id;
          const scope = extractAgentScope(payload);
          const nextTool = {
            id: toolID,
            rootRunId: payload.root_run_id,
            runId: payload.run_id || payload.root_run_id || '',
            name: payload.payload?.tool_name || 'tool',
            displayName: payload.payload?.display_name || payload.payload?.tool_name || '工具',
            risk: payload.payload?.risk || 'low',
            arguments: payload.payload?.arguments || {},
            status: payload.payload?.status || 'running',
            output: '',
            error: '',
            startedSeq: payload.root_seq,
            rootSeq: payload.root_seq,
            startedAt: payload.created_at || new Date().toISOString(),
            agent: scope.agentName,
            agentRole: scope.agentRole,
            subagentId: scope.subagentId,
          };
          const nextTools = [...next.tools.filter((item) => item.id !== toolID), nextTool];
          next = {
            ...next,
            tools: nextTools,
            runs: updateRunByID(next.runs, payload.root_run_id, {
              lastEventType: payload.type,
              lastRootSeq: payload.root_seq,
              toolCount: countToolsForRun(nextTools, payload.root_run_id),
              updatedAt: new Date().toISOString(),
            }),
          };
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'tool_output') {
          const toolID = payload.payload?.tool_call_id;
          next = {
            ...next,
            tools: next.tools.map((item) => (
              item.id === toolID
                ? { ...item, output: `${item.output || ''}${payload.payload?.delta || ''}`, rootSeq: payload.root_seq }
                : item
            )),
          };
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'todo_updated') {
          const body = payload.payload || {};
          const parsed = todosFromUpdatedEvent(body);
          const prevOpen = next.todoOpenCount || 0;
          const shouldAutoExpand =
            parsed.openCount > 0 && prevOpen === 0 && !next.todosAutoExpandedOnce;
          next = {
            ...next,
            todos: parsed.items,
            todoOpenCount: parsed.openCount,
            todosVersion: (next.todosVersion || 0) + 1,
            todosHydrated: true,
            todosExpanded: shouldAutoExpand ? true : next.todosExpanded,
            todosAutoExpandedOnce: shouldAutoExpand ? true : next.todosAutoExpandedOnce,
          };
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'goal_updated') {
          const body = payload.payload || {};
          const updated = goalFromUpdatedEvent(body);
          if (updated) {
            const goals = Array.isArray(next.goals) ? [...next.goals] : [];
            const idx = goals.findIndex((g) => g.id === updated.id);
            if (idx >= 0) goals[idx] = updated;
            else goals.unshift(updated);
            const focus = pickFocusGoal(goals);
            next = {
              ...next,
              goals,
              goal: focus,
              goalHydrated: true,
              goalExpanded: focus && (focus.status === 'active' || focus.status === 'paused')
                ? true
                : next.goalExpanded,
            };
          }
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'tool_finished' || payload.type === 'tool_failed') {
          const toolID = payload.payload?.tool_call_id;
          next = {
            ...next,
            tools: next.tools.map((item) => (
              item.id === toolID
                ? {
                    ...item,
                    status: payload.payload?.status || (payload.type === 'tool_failed' ? 'failed' : 'completed'),
                    output: payload.payload?.output || item.output,
                    error: payload.payload?.error || item.error,
                    durationMs: payload.payload?.duration_ms,
                    rootSeq: payload.root_seq,
                  }
                : item
            )),
            runs: next.runs.map((item) => (
              item.id === payload.root_run_id
                ? {
                    ...item,
                    lastEventType: payload.type,
                    lastRootSeq: payload.root_seq,
                    updatedAt: new Date().toISOString(),
                  }
                : item
            )),
          };
          if (payload.type === 'tool_finished') {
            const parsed = todosFromToolFinishedPayload(payload.payload || {});
            if (parsed) {
              const prevOpen = next.todoOpenCount || 0;
              const shouldAutoExpand =
                parsed.openCount > 0 && prevOpen === 0 && !next.todosAutoExpandedOnce;
              next = {
                ...next,
                todos: parsed.items,
                todoOpenCount: parsed.openCount,
                todosVersion: (next.todosVersion || 0) + 1,
                todosHydrated: true,
                todosExpanded: shouldAutoExpand ? true : next.todosExpanded,
                todosAutoExpandedOnce: shouldAutoExpand ? true : next.todosAutoExpandedOnce,
              };
            }
          }
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'subagent_update') {
          return { ...map, [sessionId]: next };
        }

        if (isRootTerminalRunEvent(payload)) {
          const runEventsByRun = { ...next.runEventsByRun };
          delete runEventsByRun[payload.root_run_id];
          next = {
            ...next,
            running: false,
            currentRunId: next.currentRunId === payload.root_run_id ? '' : next.currentRunId,
            runEventsByRun,
            runEventsError: { ...next.runEventsError, [payload.root_run_id]: '' },
            permissions: next.permissions.map((item) => (
              item.runId === payload.root_run_id && (item.status === 'pending' || !item.status)
                ? { ...item, status: 'closed', rootSeq: payload.root_seq }
                : item
            )),
            runs: next.runs.map((item) => (
              item.id === payload.root_run_id
                ? {
                    ...item,
                    status: payload.payload?.status || (payload.type === 'error' ? 'failed' : 'completed'),
                    lastEventType: payload.type,
                    lastRootSeq: payload.root_seq,
                    error: payload.payload?.error || item.error,
                    finishedAt: new Date().toISOString(),
                    updatedAt: new Date().toISOString(),
                  }
                : item
            )),
          };
          setGlobalPendingPermissions((items) => items.filter((item) => item.runId !== payload.root_run_id));
          if (payload.type === 'finish' && payload.payload?.status === 'cancelled') {
            next = {
              ...next,
              messages: [
                ...next.messages,
                {
                  id: payload.event_id || `cancelled_${Date.now()}`,
                  role: 'assistant',
                  agent: payload.agent?.name || 'runtime',
                  rootSeq: payload.root_seq,
                  text: '运行已取消。',
                },
              ],
            };
          }
          return { ...map, [sessionId]: next };
        }

        if (payload.type === 'skills_injected') {
          return { ...map, [sessionId]: next };
        }

        const text = payload.payload?.delta || payload.payload?.message || '';
        if (!text) {
          return { ...map, [sessionId]: next };
        }

        next = {
          ...next,
          messages: appendAgentText(next.messages, payload, text),
          runs: next.runs.map((item) => (
            item.id === payload.root_run_id
              ? {
                  ...item,
                  lastEventType: payload.type,
                  lastRootSeq: payload.root_seq,
                  messageCount: (item.messageCount || 0) + 1,
                  updatedAt: new Date().toISOString(),
                }
              : item
          )),
        };
        return { ...map, [sessionId]: next };
      });
    },
  });

  async function createSession() {
    const session = await apiJson('/api/v1/sessions', {
      method: 'POST',
      body: JSON.stringify({
        name: '新会话',
        workspace_root: workspace?.root_path || workspace?.root || '',
      }),
    });
    const normalized = normalizeSession(session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setSessionRuntimes((map) => ({
      ...map,
      [normalized.id]: createEmptySessionRuntime({ hydrated: true }),
    }));
    setCurrentSessionId(normalized.id);
    setGlobalPendingPermissions([]);
  }

  async function openWorkspaceRoot(root) {
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
    loadSkills(normalized?.root_path || normalized?.root || '');
    return normalized;
  }

  async function selectWorkspaceNode(node) {
    if (!node?.root) return;
    await openWorkspaceRoot(node.root);
  }

  async function deleteSession(session) {
    if (!session?.id) return;
    const ok = await dialog.confirm({
      title: '删除会话',
      message: `确定删除会话「${session.title || session.id}」？`,
      description: '删除后无法从侧栏恢复该会话记录。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-session',
    });
    if (!ok) return;
    await apiJson(`/api/v1/sessions/${encodeURIComponent(session.id)}`, { method: 'DELETE' });
    const remaining = sessions.filter((item) => item.id !== session.id);
    setSessions(remaining);
    setSessionRuntimes((map) => {
      const next = { ...map };
      delete next[session.id];
      return next;
    });
    if (currentSessionId === session.id) {
      if (remaining.length > 0) {
        selectSession(remaining[0].id);
      } else {
        setCurrentSessionId('');
      }
    }
  }

  async function deleteWorkspaceNode(node) {
    if (!node?.id || String(node.id).startsWith('path:')) return;
    const ok = await dialog.confirm({
      title: '移除工作区',
      message: `确定移除工作区「${node.name}」？`,
      description: '将从最近列表移除，并删除其下会话记录。磁盘文件不会被删除。',
      confirmLabel: '移除',
      tone: 'danger',
      testId: 'confirm-delete-workspace',
    });
    if (!ok) return;
    await apiJson(`/api/v1/workspaces/${encodeURIComponent(node.id)}?delete_sessions=1`, { method: 'DELETE' });
    const rootKey = (node.root || '').replace(/\\/g, '/').toLowerCase();
    const remaining = sessions.filter((item) => {
      const itemRoot = (item.workspaceRoot || '').replace(/\\/g, '/').toLowerCase();
      return itemRoot !== rootKey;
    });
    setSessions(remaining);
    const nextRecent = recentWorkspaces.filter((item) => item.id !== node.id);
    setRecentWorkspaces(nextRecent);
    const currentRoot = (workspace?.root_path || workspace?.root || '').replace(/\\/g, '/').toLowerCase();
    if (currentRoot === rootKey) {
      const nextWorkspace = nextRecent[0] || null;
      setWorkspace(nextWorkspace);
      if (nextWorkspace?.root || nextWorkspace?.root_path) {
        loadSkills(nextWorkspace.root_path || nextWorkspace.root);
      } else {
        setSkills([]);
      }
    }
    if (currentSessionId && !remaining.some((item) => item.id === currentSessionId)) {
      if (remaining.length > 0) {
        selectSession(remaining[0].id);
      } else {
        setCurrentSessionId('');
      }
    }
    setSessionRuntimes((map) => {
      const keep = new Set(remaining.map((item) => item.id));
      const next = {};
      for (const [id, runtime] of Object.entries(map)) {
        if (keep.has(id)) next[id] = runtime;
      }
      return next;
    });
  }

  async function browseWorkspaceDirectory() {
    const path = await selectDirectory();
    return path || '';
  }

  async function forkSession() {
    if (!currentSessionId) return;
    const current = sessions.find((item) => item.id === currentSessionId);
    const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/fork`, {
      method: 'POST',
      body: JSON.stringify({
        name: `${current?.title || '会话'} 的分叉`,
      }),
    });
    const normalized = normalizeSession(result.session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setCurrentSessionId(normalized.id);
    await loadSessionState(normalized.id);
  }

  /**
   * Pause this session's root run and all subagents before context summary.
   * Gateway also pauses server-side; Desktop updates local projection promptly.
   */
  async function pauseSessionForCompact(sessionId) {
    const rt = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
    const runId = rt.currentRunId || '';
    const activeSubs = (rt.subAgents || []).filter((item) => (
      item?.status === 'running' || item?.status === 'waiting_permission' || item?.status === 'cancelling'
    ));

    if (!runId && activeSubs.length === 0 && !rt.running) {
      return { paused: false, runId: '', subAgents: 0 };
    }

    appendDiagnosticLog('info', '摘要前暂停会话主代理与子代理', {
      source: 'compact',
      detail: {
        sessionId,
        runId,
        subAgentCount: activeSubs.length,
        subAgentIds: activeSubs.map((item) => item.id),
      },
    });

    // Mark local state as paused so the UI stops accepting sends immediately.
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      compacting: true,
      running: false,
      subAgents: (prev.subAgents || []).map((item) => (
        item.status === 'running' || item.status === 'waiting_permission'
          ? { ...item, status: 'cancelling', summary: '摘要前暂停' }
          : item
      )),
    }));

    // Cancel subagents first, then the root run (mirrors gateway order).
    await Promise.all(activeSubs.map((agent) => request('subagent.cancel', {
      run_id: agent.rootRunId || runId,
      subagent_id: agent.id,
      reason: 'session compact pause',
    }).catch(() => null)));

    if (runId) {
      await request('run.cancel', {
        run_id: runId,
        reason: 'session compact pause',
      }).catch(() => null);
    }

    // Wait briefly for finish events to land so history is stable.
    const deadline = Date.now() + 12_000;
    while (Date.now() < deadline) {
      const latest = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
      const stillRunning = latest.running;
      const stillActiveSubs = (latest.subAgents || []).some((item) => (
        item?.status === 'running' || item?.status === 'waiting_permission' || item?.status === 'cancelling'
      ));
      if (!stillRunning && !stillActiveSubs) break;
      await new Promise((resolve) => window.setTimeout(resolve, 120));
    }

    patchRuntime(sessionId, (prev) => ({
      ...prev,
      running: false,
      currentRunId: '',
      compacting: true,
      subAgents: (prev.subAgents || []).map((item) => (
        item.status === 'running' || item.status === 'waiting_permission' || item.status === 'cancelling'
          ? { ...item, status: 'cancelled', summary: '已为上下文摘要暂停' }
          : item
      )),
    }));

    return { paused: true, runId, subAgents: activeSubs.length };
  }

  async function compactSession({ silent = false } = {}) {
    const sessionId = currentSessionIdRef.current;
    if (!sessionId || sessionId === 'local-design') return null;

    patchRuntime(sessionId, (prev) => ({ ...prev, compacting: true }));
    try {
      const pauseInfo = await pauseSessionForCompact(sessionId);

      // Keep recent conversation rounds verbatim for the model. Full UI history remains unchanged.
      const compactBody = {
        keep_tail_turns: 3,
        mode: 'auto',
        provider_profile_id: runSettings.providerProfileId || undefined,
      };
      appendDiagnosticLog('info', '开始更新会话上下文摘要（保留最近 3 轮原文）', {
        source: 'compact',
        detail: { ...compactBody, paused: pauseInfo },
      });
      const preview = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact/preview`, {
        method: 'POST',
        body: JSON.stringify(compactBody),
      });
      const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact`, {
        method: 'POST',
        body: JSON.stringify({
          ...compactBody,
          summary: preview.preview?.summary,
        }),
      });
      patchRuntime(sessionId, (prev) => ({
        ...prev,
        compacting: false,
        running: false,
        contextSummary: result.summary || preview.preview?.summary || null,
        contextSummaryEndSeq: Number(result.compaction?.source_end_seq) || 0,
        messages: pauseInfo.paused
          ? [
              ...prev.messages,
              {
                id: `compact_pause_${Date.now()}`,
                role: 'assistant',
                agent: 'system',
                text: '已暂停当前会话与子代理并完成上下文摘要。可继续发送下一条消息。',
              },
            ]
          : prev.messages,
      }));
      appendDiagnosticLog('info', `会话上下文摘要已更新（${preview.preview?.summary_method || 'unknown'}）`, {
        source: 'compact',
        detail: {
          sessionId,
          sourceEndSeq: result.compaction?.source_end_seq,
          keepTailTurns: preview.preview?.keep_tail_turns,
          summaryMethod: preview.preview?.summary_method,
          pausedRuns: result.paused_runs ?? preview.paused_runs ?? 0,
        },
      });
      if (!silent) {
        // Manual compact stays quiet; auto path can toast via caller.
      }
      await hydrateGoals(sessionId);
      return result.compaction || null;
    } catch (error) {
      patchRuntime(sessionId, (prev) => ({ ...prev, compacting: false }));
      throw error;
    }
  }

  // Auto-compact when estimated context usage hits 90% of provider max_tokens.
  // May fire mid-run: compactSession pauses the session + subagents first.
  useEffect(() => {
    if (!contextTokenBudget.autoCompact) return;
    if (!currentSessionId || currentSessionId === 'local-design') return;
    if (autoCompactBusyRef.current) return;
    if (compacting) return;
    // Avoid re-firing on the same session until compact succeeds and usage drops.
    if (autoCompactSessionRef.current === currentSessionId) return;

    autoCompactBusyRef.current = true;
    autoCompactSessionRef.current = currentSessionId;
    compactSession({ silent: true })
      .catch(() => {
        // Allow retry after a short cool-down if compact failed.
        window.setTimeout(() => {
          if (autoCompactSessionRef.current === currentSessionId) {
            autoCompactSessionRef.current = '';
          }
        }, 8000);
      })
      .finally(() => {
        autoCompactBusyRef.current = false;
      });
  }, [contextTokenBudget.autoCompact, currentSessionId, compacting]);

  useEffect(() => {
    if (!contextTokenBudget.autoCompact) {
      autoCompactSessionRef.current = '';
    }
  }, [contextTokenBudget.autoCompact]);

  function selectSession(id) {
    setCurrentSessionId(id);
    const target = sessions.find((item) => item.id === id);
    if (target?.workspaceRoot) {
      openWorkspaceRoot(target.workspaceRoot).catch(() => {});
    }
    // Do not wipe other sessions' live state — only hydrate if needed.
    const existing = sessionRuntimes[id];
    if (!existing?.hydrated || (!existing.running && (existing.messages || []).length === 0)) {
      loadSessionState(id);
    } else {
      loadGlobalPendingPermissions();
    }
  }

  async function sendTask() {
    const sessionId = currentSessionIdRef.current;
    const text = draft.trim();
    const sessionRt = sessionRuntimes[sessionId];
    const sessionRunning = sessionRt?.running;
    const sessionCompacting = sessionRt?.compacting;
    if (!text || !sessionId || sessionRunning || sessionCompacting) return;

    const cmd = parseCommand(text);

    // Local-only commands: help / parse errors — no Gateway run.
    if (cmd.action === 'help' || cmd.action === 'error') {
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: cmd.displayText || text },
          {
            id: `cmd_${Date.now()}`,
            role: 'assistant',
            agent: 'system',
            createdAt: new Date().toISOString(),
            text: cmd.message || '指令已处理',
          },
        ],
      }));
      return;
    }

    // /goal cancel — stop goal without starting a run.
    if (cmd.action === 'cancel_goal') {
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: cmd.displayText || text },
        ],
      }));
      try {
        await cancelGoal();
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          messages: [
            ...rt.messages,
            {
              id: `cmd_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              createdAt: new Date().toISOString(),
              text: '已取消当前目标。',
            },
          ],
        }));
      } catch (error) {
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          messages: [
            ...rt.messages,
            {
              id: `cmd_err_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              text: `取消目标失败：${error.message}`,
            },
          ],
        }));
      }
      return;
    }

    // /goal continue — reuse continue endpoint.
    if (cmd.action === 'continue_goal') {
      const display = cmd.displayText || text;
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        running: true,
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: display },
        ],
      }));
      try {
        const result = await continueGoal(cmd.extraText || '', {
          require_permission: cmd.requirePermission,
          spawn_subagents: cmd.spawnSubAgents,
        });
        const nextRunId = result?.run_id || '';
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          running: true,
          currentRunId: nextRunId || rt.currentRunId,
        }));
        setRightPanelTab('activity');
        await hydrateGoals(sessionId);
      } catch (error) {
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          running: false,
          messages: [
            ...rt.messages,
            {
              id: `cmd_err_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              text: `继续目标失败：${error.message}`,
            },
          ],
        }));
      }
      return;
    }

    // Normal run or /goal <objective> (start_goal via create_goal options).
    const displayText = cmd.displayText || text;
    const inputText = cmd.inputText || text;
    const optionOverrides = {
      require_permission: cmd.requirePermission,
      spawn_subagents: cmd.spawnSubAgents,
    };
    if (cmd.action === 'start_goal') {
      optionOverrides.create_goal = true;
      optionOverrides.goal_objective = cmd.objective;
      optionOverrides.goal_title = cmd.title;
      optionOverrides.goal_success_criteria = cmd.successCriteria;
      optionOverrides.goals_enabled = true;
    }

    patchRuntime(sessionId, (rt) => ({
      ...rt,
      draft: '',
      running: true,
      subAgents: [],
      messages: [
        ...rt.messages,
        { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: displayText },
      ],
    }));
    // Refresh settings skills list so the UI matches disk; runtime also reloads per conversation.
    loadSkills().catch(() => {});

    try {
      const runOptions = buildRunStartOptions(runSettings, workspace, text, optionOverrides);
      if (runOptions.log_llm_requests) {
        appendDiagnosticLog('info', '已开启 LLM 请求记录；本次运行将写入本机诊断目录', {
          source: 'run',
          detail: { sessionId, log_llm_requests: true },
        });
      }
      const result = await request('run.start', {
        session_id: sessionId,
        input: { text: inputText },
        options: runOptions,
        subscribe: true,
      });
      const nextRunId = result?.run_id || '';
      appendDiagnosticLog(
        'info',
        cmd.action === 'start_goal'
          ? `Goal 已通过指令启动 ${nextRunId || '(无 run_id)'}`
          : `运行已启动 ${nextRunId || '(无 run_id)'}`,
        {
          source: cmd.action === 'start_goal' ? 'goal' : 'run',
          detail: {
            sessionId,
            runId: nextRunId,
            runtimeMode: result?.runtime_mode,
            command: cmd.name,
            objective: cmd.objective,
          },
        },
      );
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        running: true,
        currentRunId: nextRunId || rt.currentRunId,
        runs: nextRunId
          ? [
              normalizeRun({
                id: nextRunId,
                session_id: sessionId,
                workspace_root: workspace?.root_path || workspace?.root || '',
                runtime_mode: result?.runtime_mode || runSettings.runtimeMode,
                status: 'running',
                input: displayText,
                last_root_seq: result?.root_seq || rt.rootSeq,
                message_count: 1,
                tool_count: 0,
                started_at: new Date().toISOString(),
                updated_at: new Date().toISOString(),
              }),
              ...rt.runs.filter((item) => item.id !== nextRunId),
            ]
          : rt.runs,
      }));
      if (cmd.action === 'start_goal') {
        await hydrateGoals(sessionId);
      }
      setRightPanelTab('activity');
    } catch (error) {
      appendDiagnosticLog('error', `启动运行失败：${error.message}`, {
        source: 'run',
        detail: { sessionId },
      });
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        running: false,
        currentRunId: '',
        messages: [
          ...rt.messages,
          {
            id: `gateway_error_${Date.now()}`,
            role: 'assistant',
            agent: 'system',
            text: `启动运行失败：${error.message}`,
          },
        ],
      }));
    }
  }

  async function cancelRun() {
    setRunning(false);
    const runId = currentRunId;
    setCurrentRunId('');
    if (runId) {
      await request('run.cancel', { run_id: runId }).catch(() => {});
    }
  }

  function openSubagentConversation(agent) {
    if (!agent?.id || agent.role !== 'subagent') return;
    const tabId = subagentConversationTabId(agent.id);
    setConversationTabs((tabs) => {
      if (tabs.some((tab) => tab.id === tabId)) {
        return tabs.map((tab) => (
          tab.id === tabId
            ? {
                ...tab,
                title: agent.name || tab.title,
                status: agent.status,
                statusLabel: displayStatus(agent.status),
                runId: agent.runId || tab.runId,
              }
            : tab
        ));
      }
      return [
        ...tabs,
        {
          id: tabId,
          kind: 'subagent',
          subagentId: agent.id,
          title: agent.name || agent.id,
          status: agent.status,
          statusLabel: displayStatus(agent.status),
          runId: agent.runId || '',
          closable: true,
        },
      ];
    });
    setActiveConversationTab(tabId);
  }

  function closeConversationTab(tabId) {
    if (!tabId || tabId === 'main') return;
    setConversationTabs((tabs) => {
      const next = tabs.filter((tab) => tab.id !== tabId);
      return next.length > 0 ? next : [MAIN_CONVERSATION_TAB];
    });
    setActiveConversationTab((current) => (current === tabId ? 'main' : current));
  }

  async function cancelSubAgent(agent) {
    if (!agent?.id || !agent.rootRunId || agent.status !== 'running') {
      return;
    }
    setSubAgents((items) => items.map((item) => (
      item.id === agent.id
        ? { ...item, status: 'cancelling', summary: '已请求取消' }
        : item
    )));
    try {
      const result = await request('subagent.cancel', {
        run_id: agent.rootRunId,
        subagent_id: agent.id,
        reason: 'desktop',
      });
      if (!result?.cancelled) {
        setSubAgents((items) => items.map((item) => (
          item.id === agent.id
            ? { ...item, status: 'completed', summary: '子代理已结束' }
            : item
        )));
      }
    } catch (error) {
      setSubAgents((items) => items.map((item) => (
        item.id === agent.id
          ? { ...item, status: 'running', summary: error.message }
          : item
      )));
      setMessages((items) => [
        ...items,
        {
          id: `subagent_cancel_error_${Date.now()}`,
          role: 'assistant',
          agent: 'system',
          text: `取消子代理失败：${error.message}`,
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

  async function createProviderProfile(input) {
    const created = await apiJson('/api/v1/provider-profiles', {
      method: 'POST',
      body: JSON.stringify(providerProfileCreatePayload(input)),
    });
    const normalized = normalizeProviderProfile(created);
    setProviderProfiles((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setRunSettings((current) => ({ ...current, providerProfileId: normalized.id }));
    return normalized;
  }

  async function updateProviderProfile(id, input) {
    const updated = await apiJson(`/api/v1/provider-profiles/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(providerProfileUpdatePayload(input)),
    });
    const normalized = normalizeProviderProfile(updated);
    setProviderProfiles((items) => [
      normalized,
      ...items.filter((item) => item.id !== normalized.id),
    ]);
    return normalized;
  }

  async function deleteProviderProfile(id) {
    await apiJson(`/api/v1/provider-profiles/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setProviderProfiles((items) => items.filter((item) => item.id !== id));
    setRunSettings((current) => (
      current.providerProfileId === id
        ? { ...current, providerProfileId: '' }
        : current
    ));
  }

  async function createMcpServer(input) {
    setMcpServersError('');
    try {
      const created = await apiJson('/api/v1/mcp/servers', {
        method: 'POST',
        body: JSON.stringify(mcpServerCreatePayload(input)),
      });
      const normalized = normalizeMcpServer(created);
      setMcpServers((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }

  async function updateMcpServer(id, input) {
    setMcpServersError('');
    try {
      const updated = await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: JSON.stringify(mcpServerUpdatePayload(input)),
      });
      const normalized = normalizeMcpServer(updated);
      setMcpServers((items) => [
        normalized,
        ...items.filter((item) => item.id !== normalized.id),
      ]);
      return normalized;
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }

  async function deleteMcpServer(id) {
    setMcpServersError('');
    try {
      await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: 'DELETE' });
      setMcpServers((items) => items.filter((item) => item.id !== id));
      setMcpDiscoveryByServer((current) => {
        const next = { ...current };
        delete next[id];
        return next;
      });
    } catch (error) {
      setMcpServersError(error.message);
      throw error;
    }
  }

  async function discoverMcpServer(id) {
    setMcpDiscoveryByServer((current) => ({
      ...current,
      [id]: { ...current[id], loading: true, error: '' },
    }));
    try {
      const data = await apiJson(`/api/v1/mcp/servers/${encodeURIComponent(id)}/discover`, {
        method: 'POST',
      });
      const result = normalizeMcpDiscovery(data);
      setMcpDiscoveryByServer((current) => ({
        ...current,
        [id]: { loading: false, error: '', result },
      }));
      return result;
    } catch (error) {
      setMcpDiscoveryByServer((current) => ({
        ...current,
        [id]: { ...current[id], loading: false, error: error.message },
      }));
      throw error;
    }
  }

  async function createSkill(input) {
    setSkillsError('');
    try {
      const created = await apiJson('/api/v1/skills', {
        method: 'POST',
        body: JSON.stringify(skillCreatePayload(input, currentWorkspaceRoot())),
      });
      await loadSkills();
      return created;
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }

  async function updateSkill(name, input) {
    setSkillsError('');
    try {
      const updated = await apiJson(`/api/v1/skills/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify(skillUpdatePayload(input, currentWorkspaceRoot())),
      });
      await loadSkills();
      return updated;
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }

  async function deleteSkill(name) {
    setSkillsError('');
    const root = currentWorkspaceRoot();
    try {
      await apiJson(
        `/api/v1/skills/${encodeURIComponent(name)}?workspace_root=${encodeURIComponent(root)}`,
        { method: 'DELETE' },
      );
      setSkills((items) => items.filter((item) => item.name !== name));
    } catch (error) {
      setSkillsError(error.message);
      throw error;
    }
  }

  function selectRightPanelTab(tab) {
    setRightPanelTab(tab);
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
    window.requestAnimationFrame(() => {
      tabList.querySelector(`[data-right-panel-tab="${nextTab.id}"]`)?.focus();
    });
  }

  const rightPanelContent = rightPanelTab === 'workspace' ? (
    <WorkspacePanel apiJson={apiJson} workspace={workspace} />
  ) : rightPanelTab === 'subagents' ? (
    <SubAgentPanel
      agents={agents}
      onCancelSubAgent={cancelSubAgent}
      onOpenSubagentConversation={openSubagentConversation}
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
        agents={managedAgents}
        agentsError={managedAgentsError}
        agentsLoading={managedAgentsLoading}
        mcpServers={mcpServers}
        mcpServersError={mcpServersError}
        mcpServersLoading={mcpServersLoading}
        mcpDiscoveryByServer={mcpDiscoveryByServer}
        onCreateAgent={createAgent}
        onCreateMcpServer={createMcpServer}
        onCreateProviderProfile={createProviderProfile}
        onCreateSkill={createSkill}
        onDeleteAgent={deleteAgent}
        onDeleteMcpServer={deleteMcpServer}
        onDeleteProviderProfile={deleteProviderProfile}
        onDeleteSkill={deleteSkill}
        onDiscoverMcpServer={discoverMcpServer}
        onLoadSkillDetail={loadSkillDetail}
        onChange={setRunSettings}
        onClose={() => setSettingsOpen(false)}
        onRefreshAgents={loadAgents}
        onRefreshMcpServers={loadMcpServers}
        onRefreshProviderProfiles={loadProviderProfiles}
        onRefreshSkills={loadSkills}
        onUpdateAgent={updateAgent}
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
        className={rightPanelResizing ? 'workspace is-resizing-right' : 'workspace'}
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
          conversationTabs={conversationTabs}
          draft={draft}
          messages={messages}
          onCancel={cancelRun}
          onCloseConversationTab={closeConversationTab}
          onDraftChange={setDraft}
          onProviderProfileChange={(id) => setRunSettings((current) => ({ ...current, providerProfileId: id }))}
          onResolvePermission={resolvePermission}
          onSelectConversationTab={setActiveConversationTab}
          onSend={sendTask}
          onTodosExpandToggle={() => patchCurrentRuntime((rt) => ({
            ...rt,
            todosExpanded: !rt.todosExpanded,
          }))}
          onTodosRefresh={() => hydrateTodos(currentSessionId)}
          goal={goal}
          goalBusy={goalBusy}
          goalExpanded={goalExpanded}
          goalLoading={!goalHydrated && !goal}
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
          subAgents={subAgents}
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
        <aside
          aria-hidden={compactLayout && !rightPanelDrawerOpen ? 'true' : undefined}
          className={rightPanelDrawerOpen ? 'right-panel drawer-open' : 'right-panel'}
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
          <div className="right-panel-content" id="right-panel-content" role="tabpanel">
            {rightPanelContent}
          </div>
        </aside>
      </main>
      {lastError ? <div className="toast" role="alert">{lastError}</div> : null}
      <StatusBar
        rootSeq={rootSeq}
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
