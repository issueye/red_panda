import { useCallback } from 'react';
import { apiJson } from '../lib/api.js';
import { selectDirectory } from '../lib/desktopShell.js';
import { createEmptySessionRuntime } from '../lib/sessionRuntime.js';
import { normalizeSession } from '../lib/sessionNormalize.js';
import { normalizeWorkspaceRoot } from './sessionActionHelpers.js';

export function useSessionCrudActions({
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
  setGlobalPendingPermissions,
  loadSessionState,
  loadGlobalPendingPermissions,
  openWorkspaceRoot,
  loadSkills,
}) {
  const upsertSession = useCallback((sessionLike) => {
    const normalized = normalizeSession(sessionLike);
    if (!normalized?.id) return null;
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setSessionRuntimes((map) => (
      map[normalized.id]
        ? map
        : { ...map, [normalized.id]: createEmptySessionRuntime({ hydrated: false }) }
    ));
    return normalized;
  }, [setSessionRuntimes, setSessions]);

  const refreshSessions = useCallback(async () => {
    const items = await apiJson('/api/v1/sessions').catch(() => null);
    if (!Array.isArray(items)) return [];
    const normalized = items.map(normalizeSession).filter((item) => item?.id);
    setSessions(normalized);
    setSessionRuntimes((map) => {
      const next = { ...map };
      for (const item of normalized) {
        if (!next[item.id]) {
          next[item.id] = createEmptySessionRuntime({ hydrated: false });
        }
      }
      return next;
    });
    return normalized;
  }, [setSessionRuntimes, setSessions]);

  const selectSession = useCallback(async (id) => {
    if (!id) return;
    let target = sessions.find((item) => item.id === id);
    if (!target) {
      // Session may have been created by a scheduled task / other client.
      const refreshed = await refreshSessions();
      target = refreshed.find((item) => item.id === id) || null;
    }
    setCurrentSessionId(id);
    if (target?.workspaceRoot) {
      openWorkspaceRoot(target.workspaceRoot).catch(() => {});
    }
    const existing = sessionRuntimes[id];
    if (!existing?.hydrated || (!existing.running && (existing.messages || []).length === 0)) {
      loadSessionState(id);
    } else {
      loadGlobalPendingPermissions();
    }
  }, [
    loadGlobalPendingPermissions,
    loadSessionState,
    openWorkspaceRoot,
    refreshSessions,
    sessionRuntimes,
    sessions,
    setCurrentSessionId,
  ]);

  const createSession = useCallback(async () => {
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
  }, [setCurrentSessionId, setGlobalPendingPermissions, setSessionRuntimes, setSessions, workspace]);

  const deleteSession = useCallback(async (session) => {
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
  }, [currentSessionId, dialog, selectSession, sessions, setCurrentSessionId, setSessionRuntimes, setSessions]);

  const deleteWorkspaceNode = useCallback(async (node) => {
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
    const rootKey = normalizeWorkspaceRoot(node.root);
    const remaining = sessions.filter((item) => normalizeWorkspaceRoot(item.workspaceRoot) !== rootKey);
    setSessions(remaining);
    const nextRecent = recentWorkspaces.filter((item) => item.id !== node.id);
    setRecentWorkspaces(nextRecent);
    const currentRoot = normalizeWorkspaceRoot(workspace?.root_path || workspace?.root);
    if (currentRoot === rootKey) {
      const nextWorkspace = nextRecent[0] || null;
      setWorkspace(nextWorkspace);
      loadSkills?.(nextWorkspace?.root_path || nextWorkspace?.root || '');
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
      return Object.fromEntries(Object.entries(map).filter(([id]) => keep.has(id)));
    });
  }, [
    currentSessionId,
    dialog,
    loadSkills,
    recentWorkspaces,
    selectSession,
    sessions,
    setCurrentSessionId,
    setRecentWorkspaces,
    setSessionRuntimes,
    setSessions,
    setWorkspace,
    workspace,
  ]);

  const browseWorkspaceDirectory = useCallback(async () => {
    const path = await selectDirectory();
    return path || '';
  }, []);

  const forkSession = useCallback(async () => {
    if (!currentSessionId) return;
    const current = sessions.find((item) => item.id === currentSessionId);
    const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/fork`, {
      method: 'POST',
      body: JSON.stringify({ name: `${current?.title || '会话'} 的分叉` }),
    });
    const normalized = normalizeSession(result.session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setCurrentSessionId(normalized.id);
    await loadSessionState(normalized.id);
  }, [currentSessionId, loadSessionState, sessions, setCurrentSessionId, setSessions]);

  return {
    createSession,
    deleteSession,
    deleteWorkspaceNode,
    browseWorkspaceDirectory,
    forkSession,
    selectSession,
    upsertSession,
    refreshSessions,
  };
}
