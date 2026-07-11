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
import { gatewayBaseURL } from './lib/config.js';
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
import { buildRunStartOptions, defaultRunSettings } from './lib/runOptions.js';
import {
  normalizeSkillDetail,
  normalizeSkillsList,
  skillCreatePayload,
  skillUpdatePayload,
} from './lib/skills.js';
import { resolveSubAgentLifecycleStatus } from './lib/subagentStatus.js';

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

const MAIN_CONVERSATION_TAB = {
  id: 'main',
  kind: 'main',
  title: '主对话',
  closable: false,
};

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
  const [messages, setMessages] = useState(initialMessages);
  const [workspace, setWorkspace] = useState(null);
  const [recentWorkspaces, setRecentWorkspaces] = useState([]);
  const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false);
  const [permissions, setPermissions] = useState([]);
  const [globalPendingPermissions, setGlobalPendingPermissions] = useState([]);
  const [tools, setTools] = useState([]);
  const [runs, setRuns] = useState([]);
  const [draft, setDraft] = useState('');
  const [running, setRunning] = useState(false);
  const [currentRunId, setCurrentRunId] = useState('');
  const [rootSeq, setRootSeq] = useState(1);
  const [subAgents, setSubAgents] = useState([]);
  const [conversationTabs, setConversationTabs] = useState([MAIN_CONVERSATION_TAB]);
  const [activeConversationTab, setActiveConversationTab] = useState('main');
  const [runEventsByRun, setRunEventsByRun] = useState({});
  const [runEventsLoading, setRunEventsLoading] = useState({});
  const [runEventsError, setRunEventsError] = useState({});
  const [rightPanelTab, setRightPanelTab] = useState('workspace');
  const [rightPanelDrawerOpen, setRightPanelDrawerOpen] = useState(false);
  const [compactLayout, setCompactLayout] = useState(() => (
    typeof window !== 'undefined' && window.matchMedia('(max-width: 1100px)').matches
  ));
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [runSettings, setRunSettings] = useState(loadRunSettings);
  const [providerProfiles, setProviderProfiles] = useState([]);
  const [providerProfilesLoading, setProviderProfilesLoading] = useState(false);
  const [providerProfilesError, setProviderProfilesError] = useState('');
  const [mcpServers, setMcpServers] = useState([]);
  const [mcpServersLoading, setMcpServersLoading] = useState(false);
  const [mcpServersError, setMcpServersError] = useState('');
  const [mcpDiscoveryByServer, setMcpDiscoveryByServer] = useState({});
  const [skills, setSkills] = useState([]);
  const [skillsLoading, setSkillsLoading] = useState(false);
  const [skillsError, setSkillsError] = useState('');
  const rightPanelCloseRef = useRef(null);
  const rightPanelReturnFocusRef = useRef(null);

  const agents = useMemo(() => [
    { id: 'root', name: 'root', role: 'root', status: running ? 'running' : 'idle', seq: rootSeq },
    ...subAgents,
  ], [rootSeq, running, subAgents]);
  const pendingPermissions = useMemo(
    () => permissions.filter((item) => item.status === 'pending' || item.status === 'resolved' || !item.status),
    [permissions],
  );
  const resumeCursors = useMemo(
    () => Object.fromEntries(
      runs
        .filter((item) => item.status === 'running' || item.status === 'waiting_permission')
        .map((item) => [item.id, item.lastRootSeq || 0]),
    ),
    [runs],
  );

  useEffect(() => {
    try {
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify(runSettings));
    } catch {
      // Local storage is optional in embedded desktop previews.
    }
  }, [runSettings]);

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

  async function loadSessionState(sessionId) {
    try {
      const [history, runs, toolCalls, permissionItems] = await Promise.all([
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/history`),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/runs`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/tools`).catch(() => []),
        apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/permissions`).catch(() => []),
      ]);
      const normalized = Array.isArray(history) ? history.map(normalizeHistoryMessage) : [];
      const normalizedRuns = Array.isArray(runs) ? runs.map(normalizeRun) : [];
      setMessages(normalized.length > 0 ? normalized : []);
      setTools(Array.isArray(toolCalls) ? toolCalls.map(normalizeToolCall) : []);
      setPermissions(Array.isArray(permissionItems)
        ? permissionItems.map(normalizePermission)
        : []);
      setRuns(normalizedRuns);
      const activeRun = latestActiveRun(runs);
      setRunning(Boolean(activeRun));
      setCurrentRunId(activeRun?.id || '');
      setRootSeq((current) => latestRootSeq(runs, current));
      loadGlobalPendingPermissions();
    } catch {
      setMessages([]);
      setTools([]);
      setPermissions([]);
      setGlobalPendingPermissions([]);
      setRuns([]);
      setRunEventsByRun({});
      setRunEventsLoading({});
      setRunEventsError({});
      setRunning(false);
      setCurrentRunId('');
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
    // 网关事件按类型增量合并到本地投影，避免流式输出时反复拉取整段会话。
    onEvent: (event) => {
      const payload = event.payload || {};
      setRootSeq((current) => Math.max(current, payload.root_seq || current));

      const agent = payload.agent || {};
      if (agent.role === 'subagent' || payload.type === 'subagent_update') {
        setRightPanelTab('subagents');
        const id = agent.subagent_id || payload.payload?.subagent_id || agent.agent_id;
        if (id) {
          setSubAgents((items) => {
            const current = items.find((item) => item.id === id);
            const nextStatus = resolveSubAgentLifecycleStatus(
              payload.type,
              payload.payload,
              current?.status,
            );
            // Only replace summary on lifecycle updates; tool events often carry unrelated text.
            const nextSummary = payload.type === 'subagent_update'
              ? (payload.payload?.summary || current?.summary || '')
              : (current?.summary || payload.payload?.summary || '');
            const next = {
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
            // Keep open conversation tab titles/status in sync.
            setConversationTabs((tabs) => tabs.map((tab) => (
              tab.subagentId === id
                ? {
                    ...tab,
                    title: next.name || tab.title,
                    status: next.status,
                    statusLabel: displayStatus(next.status),
                    runId: next.runId || tab.runId,
                  }
                : tab
            )));
            return current
              ? items.map((item) => (item.id === id ? next : item))
              : [...items, next];
          });
        }
      }

      if (payload.type === 'permission_required') {
        const permissionID = payload.payload?.permission_id || `perm_${Date.now()}`;
        const scope = extractAgentScope(payload);
        const nextPermission = {
          id: permissionID,
          runId: payload.payload?.run_id || payload.root_run_id,
          sessionId: payload.session_id,
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
        setPermissions((items) => upsertByID(items, nextPermission));
        setGlobalPendingPermissions((items) => upsertByID(items, nextPermission));
        setRuns((items) => items.map((item) => (
          item.id === (payload.payload?.run_id || payload.root_run_id)
            ? {
                ...item,
                status: 'waiting_permission',
                lastEventType: payload.type,
                lastRootSeq: payload.root_seq,
                updatedAt: new Date().toISOString(),
              }
            : item
        )));
        setRightPanelTab('activity');
        return;
      }

      if (payload.type === 'tool_started') {
        const toolID = payload.payload?.tool_call_id || payload.event_id;
        const scope = extractAgentScope(payload);
        setTools((items) => {
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
          const nextTools = [
            ...items.filter((item) => item.id !== toolID),
            nextTool,
          ];
          setRuns((runItems) => updateRunByID(runItems, payload.root_run_id, {
            lastEventType: payload.type,
            lastRootSeq: payload.root_seq,
            toolCount: countToolsForRun(nextTools, payload.root_run_id),
            updatedAt: new Date().toISOString(),
          }));
          return nextTools;
        });
        return;
      }

      if (payload.type === 'tool_output') {
        const toolID = payload.payload?.tool_call_id;
        setTools((items) => items.map((item) => (
          item.id === toolID
            ? { ...item, output: `${item.output || ''}${payload.payload?.delta || ''}`, rootSeq: payload.root_seq }
            : item
        )));
        return;
      }

      if (payload.type === 'tool_finished' || payload.type === 'tool_failed') {
        const toolID = payload.payload?.tool_call_id;
        setTools((items) => items.map((item) => (
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
        )));
        setRuns((items) => items.map((item) => (
          item.id === payload.root_run_id
            ? {
                ...item,
                lastEventType: payload.type,
                lastRootSeq: payload.root_seq,
                updatedAt: new Date().toISOString(),
              }
            : item
        )));
        return;
      }

      if (payload.type === 'subagent_update') {
        return;
      }

      if (payload.type === 'finish' || payload.type === 'error') {
        setRunning(false);
        setCurrentRunId('');
        setRunEventsByRun((items) => {
          const next = { ...items };
          delete next[payload.root_run_id];
          return next;
        });
        setRunEventsError((items) => ({ ...items, [payload.root_run_id]: '' }));
        setPermissions((items) => items.map((item) => (
          item.runId === payload.root_run_id && (item.status === 'pending' || !item.status)
            ? { ...item, status: 'closed', rootSeq: payload.root_seq }
            : item
        )));
        setGlobalPendingPermissions((items) => items.filter((item) => item.runId !== payload.root_run_id));
        setRuns((items) => items.map((item) => (
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
        )));
      }

      if (payload.type === 'finish') {
        if (payload.payload?.status === 'cancelled') {
          setMessages((items) => [
            ...items,
            {
              id: payload.event_id || `cancelled_${Date.now()}`,
              role: 'assistant',
              agent: payload.agent?.name || 'runtime',
              rootSeq: payload.root_seq,
              text: '运行已取消。',
            },
          ]);
        }
        return;
      }

      const text = payload.payload?.delta || payload.payload?.message || '';
      if (!text) {
        return;
      }

      setMessages((items) => appendAgentText(items, payload, text));
      setRuns((items) => items.map((item) => (
        item.id === payload.root_run_id
          ? {
              ...item,
              lastEventType: payload.type,
              lastRootSeq: payload.root_seq,
              messageCount: (item.messageCount || 0) + 1,
              updatedAt: new Date().toISOString(),
            }
          : item
      )));
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
    setCurrentSessionId(normalized.id);
    setMessages([]);
    setPermissions([]);
    setGlobalPendingPermissions([]);
    setTools([]);
    setRuns([]);
    setRunEventsByRun({});
    setRunEventsLoading({});
    setRunEventsError({});
    setSubAgents([]);
    setConversationTabs([MAIN_CONVERSATION_TAB]);
    setActiveConversationTab('main');
    setRunning(false);
    setCurrentRunId('');
    setRootSeq(1);
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
    if (currentSessionId === session.id) {
      if (remaining.length > 0) {
        selectSession(remaining[0].id);
      } else {
        setCurrentSessionId('');
        setMessages([]);
        setPermissions([]);
        setTools([]);
        setRuns([]);
        setRunEventsByRun({});
        setSubAgents([]);
        setRunning(false);
        setCurrentRunId('');
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
        setMessages([]);
        setPermissions([]);
        setTools([]);
        setRuns([]);
        setRunning(false);
        setCurrentRunId('');
      }
    }
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

  async function compactSession() {
    if (!currentSessionId) return;
    const current = sessions.find((item) => item.id === currentSessionId);
    const preview = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/compact/preview`, {
      method: 'POST',
      body: JSON.stringify({ keep_tail_messages: 1 }),
    });
    const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/compact`, {
      method: 'POST',
      body: JSON.stringify({
        name: `${current?.title || '会话'} 的压缩版`,
        keep_tail_messages: 1,
        summary: preview.preview?.summary,
      }),
    });
    const normalized = normalizeSession(result.session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setCurrentSessionId(normalized.id);
    await loadSessionState(normalized.id);
  }

  function selectSession(id) {
    setCurrentSessionId(id);
    setPermissions([]);
    setGlobalPendingPermissions([]);
    setTools([]);
    setRuns([]);
    setRunEventsByRun({});
    setRunEventsLoading({});
    setRunEventsError({});
    setSubAgents([]);
    setConversationTabs([MAIN_CONVERSATION_TAB]);
    setActiveConversationTab('main');
    setRunning(false);
    setCurrentRunId('');
    const target = sessions.find((item) => item.id === id);
    if (target?.workspaceRoot) {
      openWorkspaceRoot(target.workspaceRoot).catch(() => {});
    }
    loadSessionState(id);
  }

  async function sendTask() {
    const text = draft.trim();
    if (!text || running) return;
    setDraft('');
    setRunning(true);
    setSubAgents([]);
    setMessages((items) => [
      ...items,
      { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text },
    ]);
    // Refresh settings skills list so the UI matches disk; runtime also reloads per conversation.
    loadSkills().catch(() => {});

    try {
      const result = await request('run.start', {
        session_id: currentSessionId,
        input: { text },
        options: buildRunStartOptions(runSettings, workspace, text),
        subscribe: true,
      });
      const nextRunId = result?.run_id || '';
      setCurrentRunId(nextRunId);
      if (nextRunId) {
        setRuns((items) => [
          normalizeRun({
            id: nextRunId,
            session_id: currentSessionId,
            workspace_root: workspace?.root_path || workspace?.root || '',
            runtime_mode: result?.runtime_mode || runSettings.runtimeMode,
            status: 'running',
            input: text,
            last_root_seq: result?.root_seq || rootSeq,
            message_count: 1,
            tool_count: 0,
            started_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          }),
          ...items.filter((item) => item.id !== nextRunId),
        ]);
      }
      setRightPanelTab('activity');
    } catch (error) {
      setMessages((items) => [
        ...items,
        {
          id: `gateway_error_${Date.now()}`,
          role: 'assistant',
          agent: 'system',
          text: `启动运行失败：${error.message}`,
        },
      ]);
      setRunning(false);
      setCurrentRunId('');
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
        gatewayBase={gatewayBase}
        onReconnect={reconnect}
        onSettings={() => setSettingsOpen(true)}
        status={status}
      />
      <SettingsPanel
        mcpServers={mcpServers}
        mcpServersError={mcpServersError}
        mcpServersLoading={mcpServersLoading}
        mcpDiscoveryByServer={mcpDiscoveryByServer}
        onCreateMcpServer={createMcpServer}
        onCreateProviderProfile={createProviderProfile}
        onCreateSkill={createSkill}
        onDeleteMcpServer={deleteMcpServer}
        onDeleteProviderProfile={deleteProviderProfile}
        onDeleteSkill={deleteSkill}
        onDiscoverMcpServer={discoverMcpServer}
        onLoadSkillDetail={loadSkillDetail}
        onChange={setRunSettings}
        onClose={() => setSettingsOpen(false)}
        onRefreshMcpServers={loadMcpServers}
        onRefreshProviderProfiles={loadProviderProfiles}
        onRefreshSkills={loadSkills}
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
      <main className="workspace">
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
          permissions={pendingPermissions}
          providerProfileId={runSettings.providerProfileId}
          providerProfiles={providerProfiles}
          running={running}
          subAgents={subAgents}
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
        runtimeStatus={running ? `运行中（${displayRuntimeMode(runSettings.runtimeMode)}）` : `${displayRuntimeMode(runSettings.runtimeMode)} 待命`}
        status={status}
      />
    </div>
  );
}
