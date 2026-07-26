/**
 * Normalize a workspace root for grouping (case-insensitive on Windows paths).
 * @param {string} root
 * @returns {string}
 */
export function normalizeWorkspaceRoot(root) {
  if (!root) return '';
  return String(root).replace(/[\\/]+$/, '').replace(/\\/g, '/').toLowerCase();
}

/**
 * Base name of a path for display.
 * @param {string} root
 * @returns {string}
 */
export function workspaceDisplayName(root) {
  if (!root) return '未绑定工作区';
  const normalized = String(root).replace(/[\\/]+$/, '').replace(/\\/g, '/');
  const parts = normalized.split('/').filter(Boolean);
  return parts[parts.length - 1] || normalized;
}

/**
 * Build a workspace → sessions tree for the sidebar.
 * @param {Array<{ id: string, workspaceRoot?: string, title?: string }>} sessions
 * @param {Array<{ id?: string, root?: string, root_path?: string, name?: string }>} workspaces
 * @param {{ id?: string, root?: string, root_path?: string, name?: string } | null} currentWorkspace
 * @returns {Array<{ key: string, id: string, root: string, name: string, sessions: any[], isCurrent: boolean }>}
 */
export function buildWorkspaceSessionTree(sessions, workspaces = [], currentWorkspace = null) {
  const nodes = new Map();

  function ensureNode(root, meta = {}) {
    const key = normalizeWorkspaceRoot(root) || '__none__';
    if (!nodes.has(key)) {
      nodes.set(key, {
        key,
        id: meta.id || (root ? `path:${root}` : 'ws_none'),
        root: root || '',
        name: meta.name || workspaceDisplayName(root),
        sessions: [],
        isCurrent: false,
      });
    } else {
      const existing = nodes.get(key);
      if (meta.id && (String(existing.id).startsWith('path:') || existing.id === 'ws_none')) {
        existing.id = meta.id;
      }
      // Prefer explicit workspace names over path basenames from sessions.
      if (meta.name && meta.id) {
        existing.name = meta.name;
      }
    }
    return nodes.get(key);
  }

  for (const workspace of workspaces) {
    const root = workspace.root_path || workspace.root || workspace.rootPath || '';
    if (!root) continue;
    ensureNode(root, {
      id: workspace.id,
      name: workspace.name || workspaceDisplayName(root),
    });
  }

  if (currentWorkspace) {
    const root = currentWorkspace.root_path || currentWorkspace.root || currentWorkspace.rootPath || '';
    if (root) {
      const node = ensureNode(root, {
        id: currentWorkspace.id,
        name: currentWorkspace.name || workspaceDisplayName(root),
      });
      node.isCurrent = true;
    }
  }

  for (const session of sessions) {
    const root = session.workspaceRoot || session.workspace_root || '';
    const node = ensureNode(root);
    node.sessions.push(session);
  }

  const ordered = Array.from(nodes.values()).filter(
    (node) => node.sessions.length > 0 || node.root || node.isCurrent,
  );

  // Preserve the caller's workspace list order (recent list). Never pin the
  // active/current workspace to the top — selecting a session must not reshuffle
  // the sidebar tree (isCurrent is still used for styling only).
  const orderIndex = new Map();
  workspaces.forEach((workspace, index) => {
    const root = workspace.root_path || workspace.root || workspace.rootPath || '';
    const key = normalizeWorkspaceRoot(root);
    if (key && !orderIndex.has(key)) {
      orderIndex.set(key, index);
    }
  });

  ordered.sort((a, b) => {
    if (!a.root && b.root) return 1;
    if (a.root && !b.root) return -1;
    const ai = orderIndex.has(a.key) ? orderIndex.get(a.key) : Number.MAX_SAFE_INTEGER;
    const bi = orderIndex.has(b.key) ? orderIndex.get(b.key) : Number.MAX_SAFE_INTEGER;
    if (ai !== bi) return ai - bi;
    return a.name.localeCompare(b.name, 'zh-CN');
  });

  for (const node of ordered) {
    node.sessions.sort((a, b) => (a.title || '').localeCompare(b.title || '', 'zh-CN'));
  }

  return ordered;
}
