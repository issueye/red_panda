import { useEffect, useMemo, useState } from 'react';
import { ChatPanel } from './components/chat/ChatPanel.jsx';
import { MemoryPanel } from './components/MemoryPanel.jsx';
import { RunActivityPanel } from './components/RunActivityPanel.jsx';
import { SettingsPanel } from './components/SettingsPanel.jsx';
import { Sidebar } from './components/Sidebar.jsx';
import { StatusBar } from './components/StatusBar.jsx';
import { SubAgentPanel } from './components/SubAgentPanel.jsx';
import { TopBar } from './components/TopBar.jsx';
import { WorkspacePanel } from './components/WorkspacePanel.jsx';
import { useGatewayConnection } from './hooks/useGatewayConnection.js';
import { normalizeRunEvent } from './lib/activityEvents.js';
import { gatewayBaseURL } from './lib/config.js';
import {
  normalizeProviderProfile,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
} from './lib/providerProfiles.js';
import { buildRunStartOptions, defaultRunSettings } from './lib/runOptions.js';

const gatewayBase = gatewayBaseURL();

const initialSessions = [
  { id: 'local-design', title: 'Architecture', subtitle: 'WebSocket / JSON-RPC / subagents' },
];

const initialMessages = [
  {
    id: 'm1',
    role: 'assistant',
    agent: 'root',
    rootSeq: 1,
    text: 'Desktop is connected to the gateway protocol. Start a task, add /permission to test approvals, or /subagent to test subagent output.',
  },
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
  const detail = session.working_dir || session.workspace_root || session.status || 'active';
  return {
    id: session.id,
    title: session.title || session.name || session.id,
    subtitle: kind ? `${kind} - ${detail}` : detail,
    kind,
    parentId: session.parent_id || '',
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
    rootSeq: message.seq || 0,
    text: firstText || '',
  };
}

function normalizeToolCall(item) {
  return {
    id: item.id,
    rootRunId: item.root_run_id,
    name: item.tool_name || 'tool',
    displayName: item.display_name || item.tool_name || 'Tool',
    risk: item.risk || 'low',
    arguments: item.arguments || {},
    status: item.status || 'running',
    output: item.output || '',
    error: item.error || '',
    durationMs: item.duration_ms,
    rootSeq: item.finished_seq || item.started_seq || 0,
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
    summary: item.summary || 'Permission required',
    detail: item.detail || item.tool_name || 'Agent Runtime is waiting for a decision.',
    risk: item.risk,
    toolName: item.tool_name,
    arguments: item.arguments || {},
    rootSeq: item.root_seq || 0,
  };
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
  const [sessions, setSessions] = useState(initialSessions);
  const [currentSessionId, setCurrentSessionId] = useState('local-design');
  const [messages, setMessages] = useState(initialMessages);
  const [workspace, setWorkspace] = useState(null);
  const [permissions, setPermissions] = useState([]);
  const [globalPendingPermissions, setGlobalPendingPermissions] = useState([]);
  const [tools, setTools] = useState([]);
  const [runs, setRuns] = useState([]);
  const [draft, setDraft] = useState('');
  const [running, setRunning] = useState(false);
  const [currentRunId, setCurrentRunId] = useState('');
  const [rootSeq, setRootSeq] = useState(1);
  const [subAgents, setSubAgents] = useState([]);
  const [runEventsByRun, setRunEventsByRun] = useState({});
  const [runEventsLoading, setRunEventsLoading] = useState({});
  const [runEventsError, setRunEventsError] = useState({});
  const [rightPanelTab, setRightPanelTab] = useState('workspace');
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [runSettings, setRunSettings] = useState(loadRunSettings);
  const [providerProfiles, setProviderProfiles] = useState([]);
  const [providerProfilesLoading, setProviderProfilesLoading] = useState(false);
  const [providerProfilesError, setProviderProfilesError] = useState('');

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
        setWorkspace(data.workspace || null);
        const nextSessions = Array.isArray(data.sessions) ? data.sessions.map(normalizeSession) : [];
        if (nextSessions.length > 0) {
          setSessions(nextSessions);
          setCurrentSessionId(nextSessions[0].id);
          loadSessionState(nextSessions[0].id);
        } else {
          loadGlobalPendingPermissions();
        }
        loadProviderProfiles();
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (settingsOpen) {
      loadProviderProfiles();
    }
  }, [settingsOpen]);

  const { status, lastError, request, reconnect } = useGatewayConnection({
    baseUrl: gatewayBase,
    resumeCursors,
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
            const next = {
              id,
              role: 'subagent',
              name: payload.payload?.name || agent.name || current?.name || id,
              status: payload.payload?.status || current?.status || 'running',
              backend: payload.payload?.backend || current?.backend || 'in_process',
              rootRunId: payload.root_run_id || current?.rootRunId || '',
              runId: payload.run_id || current?.runId || '',
              parentRunId: payload.parent_run_id || current?.parentRunId || '',
              summary: payload.payload?.summary || current?.summary || '',
              seq: payload.agent_seq || current?.seq || 0,
            };
            return current
              ? items.map((item) => (item.id === id ? next : item))
              : [...items, next];
          });
        }
      }

      if (payload.type === 'permission_required') {
        const permissionID = payload.payload?.permission_id || `perm_${Date.now()}`;
        const nextPermission = {
          id: permissionID,
          runId: payload.payload?.run_id || payload.root_run_id,
          sessionId: payload.session_id,
          status: 'pending',
          summary: payload.payload?.summary || 'Permission required',
          detail: payload.payload?.detail || payload.payload?.tool_name || 'Agent Runtime is waiting for a decision.',
          risk: payload.payload?.risk,
          toolName: payload.payload?.tool_name,
          arguments: payload.payload?.arguments || {},
          rootSeq: payload.root_seq,
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
        setTools((items) => {
          const nextTool = {
            id: toolID,
            rootRunId: payload.root_run_id,
            name: payload.payload?.tool_name || 'tool',
            displayName: payload.payload?.display_name || payload.payload?.tool_name || 'Tool',
            risk: payload.payload?.risk || 'low',
            arguments: payload.payload?.arguments || {},
            status: payload.payload?.status || 'running',
            output: '',
            error: '',
            rootSeq: payload.root_seq,
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
              text: 'Run cancelled.',
            },
          ]);
        }
        return;
      }

      const text = payload.payload?.delta || payload.payload?.message || '';
      if (!text) {
        return;
      }

      setMessages((items) => [
        ...items,
        {
          id: payload.event_id || `evt_${Date.now()}`,
          role: 'assistant',
          agent: payload.agent?.name || payload.agent?.role || 'agent',
          rootSeq: payload.root_seq,
          text,
        },
      ]);
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
        name: 'New session',
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
    setRunning(false);
    setCurrentRunId('');
    setRootSeq(1);
  }

  async function forkSession() {
    if (!currentSessionId) return;
    const current = sessions.find((item) => item.id === currentSessionId);
    const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/fork`, {
      method: 'POST',
      body: JSON.stringify({
        name: `Fork of ${current?.title || 'session'}`,
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
        name: `Compact of ${current?.title || 'session'}`,
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
    setRunning(false);
    setCurrentRunId('');
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
      { id: `user_${Date.now()}`, role: 'user', text },
    ]);

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
          agent: 'gateway',
          text: `run.start failed: ${error.message}`,
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

  async function cancelSubAgent(agent) {
    if (!agent?.id || !agent.rootRunId || agent.status !== 'running') {
      return;
    }
    setSubAgents((items) => items.map((item) => (
      item.id === agent.id
        ? { ...item, status: 'cancelling', summary: 'Cancellation requested' }
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
            ? { ...item, status: 'completed', summary: 'Subagent was already finished' }
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
          agent: 'gateway',
          text: `subagent.cancel failed: ${error.message}`,
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

  return (
    <div className="app-shell">
      <TopBar
        gatewayBase={gatewayBase}
        onReconnect={reconnect}
        onSettings={() => setSettingsOpen(true)}
        status={status}
      />
      <SettingsPanel
        onCreateProviderProfile={createProviderProfile}
        onDeleteProviderProfile={deleteProviderProfile}
        onChange={setRunSettings}
        onClose={() => setSettingsOpen(false)}
        onRefreshProviderProfiles={loadProviderProfiles}
        onUpdateProviderProfile={updateProviderProfile}
        open={settingsOpen}
        providerProfiles={providerProfiles}
        providerProfilesError={providerProfilesError}
        providerProfilesLoading={providerProfilesLoading}
        settings={runSettings}
      />
      <main className="workspace">
        <Sidebar
          currentSessionId={currentSessionId}
          onCompactSession={compactSession}
          onForkSession={forkSession}
          onNewSession={createSession}
          onSelectSession={selectSession}
          sessions={sessions}
          workspace={workspace}
        />
        <ChatPanel
          draft={draft}
          messages={messages}
          onCancel={cancelRun}
          onDraftChange={setDraft}
          onResolvePermission={resolvePermission}
          onSend={sendTask}
          permissions={pendingPermissions}
          running={running}
          tools={tools}
        />
        <aside className="right-panel">
          <div className="right-panel-tabs">
            <button
              className={rightPanelTab === 'workspace' ? 'right-tab active' : 'right-tab'}
              onClick={() => setRightPanelTab('workspace')}
              type="button"
            >
              Workspace
            </button>
            <button
              className={rightPanelTab === 'subagents' ? 'right-tab active' : 'right-tab'}
              onClick={() => setRightPanelTab('subagents')}
              type="button"
            >
              Subagents
            </button>
            <button
              className={rightPanelTab === 'activity' ? 'right-tab active' : 'right-tab'}
              data-testid="right-tab-activity"
              onClick={() => setRightPanelTab('activity')}
              type="button"
            >
              Activity
            </button>
            <button
              className={rightPanelTab === 'memory' ? 'right-tab active' : 'right-tab'}
              data-testid="right-tab-memory"
              onClick={() => setRightPanelTab('memory')}
              type="button"
            >
              Memory
            </button>
          </div>
          {rightPanelTab === 'workspace' ? (
            <WorkspacePanel apiJson={apiJson} workspace={workspace} />
          ) : rightPanelTab === 'subagents' ? (
            <SubAgentPanel agents={agents} onCancelSubAgent={cancelSubAgent} />
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
          )}
        </aside>
      </main>
      {lastError ? <div className="toast">{lastError}</div> : null}
      <StatusBar
        rootSeq={rootSeq}
        runtimeStatus={running ? `running (${runSettings.runtimeMode})` : `${runSettings.runtimeMode} standby`}
        status={status}
      />
    </div>
  );
}
