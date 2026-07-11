import {
  ChevronDown,
  ChevronRight,
  FolderOpen,
  FolderPlus,
  GitFork,
  MessageSquare,
  MessageSquarePlus,
  Minimize2,
  Trash2,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { buildWorkspaceSessionTree } from '../lib/sessionTree.js';
import { classNames } from '../lib/format.js';
import { Button, IconButton } from './ui/button.jsx';

/**
 * 侧栏：工作区 → 会话树，支持打开工作区、展开/收起与删除。
 */
export function Sidebar({
  sessions,
  currentSessionId,
  workspace,
  workspaces = [],
  sessionRunStatus = {},
  onCompactSession,
  onDeleteSession,
  onDeleteWorkspace,
  onForkSession,
  onNewSession,
  onOpenWorkspace,
  onSelectSession,
  onSelectWorkspace,
}) {
  const tree = useMemo(
    () => buildWorkspaceSessionTree(sessions, workspaces, workspace),
    [sessions, workspaces, workspace],
  );

  const [expanded, setExpanded] = useState(() => new Set());

  useEffect(() => {
    setExpanded((current) => {
      const next = new Set(current);
      for (const node of tree) {
        if (node.isCurrent || node.sessions.some((item) => item.id === currentSessionId)) {
          next.add(node.key);
        }
        if (current.size === 0) {
          next.add(node.key);
        }
      }
      return next;
    });
  }, [tree, currentSessionId]);

  const hasCurrentSession = Boolean(currentSessionId);

  function toggleNode(key) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  return (
    <aside className="sidebar">
      <Button className="sidebar-primary" icon={<MessageSquarePlus size={14} />} onClick={onNewSession} variant="default">
        新建会话
      </Button>
      <div className="session-command-row">
        <Button data-testid="session-fork" disabled={!hasCurrentSession} icon={<GitFork size={13} />} onClick={onForkSession} variant="ghost">
          分叉
        </Button>
        <Button data-testid="session-compact" disabled={!hasCurrentSession} icon={<Minimize2 size={13} />} onClick={onCompactSession} variant="ghost">
          压缩
        </Button>
      </div>
      <Button
        className="sidebar-workspace-open"
        data-testid="workspace-open"
        icon={<FolderPlus size={14} />}
        onClick={onOpenWorkspace}
        variant="soft"
      >
        选择工作区
      </Button>

      <section className="sidebar-section grow" data-testid="workspace-session-tree">
        {tree.length === 0 ? (
          <p className="sidebar-empty">暂无会话。可先选择工作区，再新建会话。</p>
        ) : (
          <div className="session-tree">
            {tree.map((node) => {
              const isOpen = expanded.has(node.key);
              const canDeleteWorkspace = Boolean(node.id) && !String(node.id).startsWith('path:') && node.root;
              return (
                <div className={classNames('tree-workspace', node.isCurrent && 'is-current')} key={node.key}>
                  <div className="tree-workspace-row">
                    <button
                      aria-expanded={isOpen}
                      className="tree-workspace-toggle"
                      onClick={() => toggleNode(node.key)}
                      type="button"
                    >
                      {isOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                    </button>
                    <button
                      className="tree-workspace-main"
                      onClick={() => {
                        if (!isOpen) toggleNode(node.key);
                        onSelectWorkspace?.(node);
                      }}
                      title={node.root || node.name}
                      type="button"
                    >
                      <FolderOpen size={14} />
                      <span className="tree-workspace-name">{node.name}</span>
                      <em>{node.sessions.length}</em>
                    </button>
                    {canDeleteWorkspace ? (
                      <IconButton
                        className="tree-delete"
                        data-testid="workspace-delete"
                        label={`移除工作区 ${node.name}`}
                        onClick={(event) => {
                          event.stopPropagation();
                          onDeleteWorkspace?.(node);
                        }}
                        variant="ghost"
                      >
                        <Trash2 size={13} />
                      </IconButton>
                    ) : null}
                  </div>
                  {isOpen ? (
                    <div className="tree-session-list">
                      {node.sessions.length === 0 ? (
                        <p className="tree-session-empty">暂无会话</p>
                      ) : (
                        node.sessions.map((session) => {
                          const runStatus = sessionRunStatus[session.id] || 'idle';
                          return (
                          <div
                            className={classNames(
                              'tree-session-row',
                              session.id === currentSessionId && 'active',
                              runStatus !== 'idle' && `is-${runStatus}`,
                            )}
                            key={session.id}
                          >
                            <button
                              className="tree-session-main"
                              data-testid="session-item"
                              onClick={() => onSelectSession(session.id)}
                              title={
                                runStatus === 'running'
                                  ? `${session.title}（运行中）`
                                  : runStatus === 'waiting_permission'
                                    ? `${session.title}（等待授权）`
                                    : session.title
                              }
                              type="button"
                            >
                              <MessageSquare size={13} />
                              <strong>{session.title}</strong>
                              {runStatus !== 'idle' ? (
                                <em
                                  className={classNames('session-run-dot', `is-${runStatus}`)}
                                  data-testid="session-run-status"
                                >
                                  {runStatus === 'waiting_permission' ? '授权' : '运行'}
                                </em>
                              ) : null}
                            </button>
                            <IconButton
                              className="tree-delete"
                              data-testid="session-delete"
                              label={`删除会话 ${session.title}`}
                              onClick={(event) => {
                                event.stopPropagation();
                                onDeleteSession?.(session);
                              }}
                              variant="ghost"
                            >
                              <Trash2 size={12} />
                            </IconButton>
                          </div>
                          );
                        })
                      )}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </section>
    </aside>
  );
}
