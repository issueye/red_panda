import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
  FolderOpen,
  FolderPlus,
  LoaderCircle,
  MessageSquare,
  MessageSquarePlus,
  ShieldAlert,
  Trash2,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { openInExplorer } from '../lib/desktopShell.js';
import { buildWorkspaceSessionTree } from '../lib/sessionTree.js';
import { classNames } from '../lib/format.js';
import { Button, IconButton } from './ui/button.jsx';
import { TabButton } from './ui/tabs.jsx';

export const leftPanelTabs = [
  { id: 'sessions', label: '会话', testId: 'left-tab-sessions' },
  { id: 'workspace', label: '工作区', testId: 'left-tab-workspace' },
];

/**
 * 左侧栏：会话树 / 工作区，通过顶部标签切换。
 */
export function Sidebar({
  sessions,
  currentSessionId,
  workspace,
  workspaces = [],
  sessionRunStatus = {},
  leftTab = 'sessions',
  onLeftTabChange,
  workspacePanel = null,
  workspaceExpanded = false,
  onDeleteSession,
  onDeleteWorkspace,
  onNewSession,
  onOpenWorkspace,
  onSelectSession,
  onSelectWorkspace,
  onWorkspaceExpandedKeyDown,
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

  const isWorkspaceWindow = leftTab === 'workspace' && workspaceExpanded;

  function toggleNode(key) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  function handleLeftTabsKeyDown(event) {
    const currentIndex = leftPanelTabs.findIndex((tab) => tab.id === leftTab);
    if (currentIndex < 0) return;

    const lastIndex = leftPanelTabs.length - 1;
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
    const nextTab = leftPanelTabs[nextIndex];
    onLeftTabChange?.(nextTab.id);
    window.requestAnimationFrame(() => {
      tabList.querySelector(`[data-left-panel-tab="${nextTab.id}"]`)?.focus();
    });
  }

  return (
    <aside
      aria-label={isWorkspaceWindow ? '工作区' : '左侧栏'}
      aria-modal={isWorkspaceWindow ? 'true' : undefined}
      className={classNames(
        'sidebar',
        isWorkspaceWindow && 'workspace-window',
      )}
      data-testid={isWorkspaceWindow ? 'workspace-dialog' : 'left-panel'}
      onKeyDown={isWorkspaceWindow ? onWorkspaceExpandedKeyDown : undefined}
      role={isWorkspaceWindow ? 'dialog' : undefined}
    >
      {!isWorkspaceWindow ? (
        <div
          aria-label="左侧栏"
          className="left-panel-tabs"
          onKeyDown={handleLeftTabsKeyDown}
          role="tablist"
        >
          {leftPanelTabs.map((tab) => (
            <TabButton
              active={leftTab === tab.id}
              data-left-panel-tab={tab.id}
              data-testid={tab.testId}
              key={tab.id}
              onClick={() => onLeftTabChange?.(tab.id)}
              panelId="left-panel-content"
            >
              {tab.label}
            </TabButton>
          ))}
        </div>
      ) : null}

      <div
        className="left-panel-content"
        id="left-panel-content"
        role={isWorkspaceWindow ? undefined : 'tabpanel'}
      >
        {leftTab === 'workspace' ? (
          workspacePanel
        ) : (
          <div className="sidebar-sessions">
            <Button className="sidebar-primary" icon={<MessageSquarePlus size={14} />} onClick={onNewSession} variant="default">
              新建会话
            </Button>
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
                          <div className="tree-workspace-actions">
                            {node.root ? (
                              <IconButton
                                className="tree-action"
                                data-testid="workspace-open-explorer"
                                label={`在文件资源管理器中打开 ${node.name}`}
                                onClick={(event) => {
                                  event.stopPropagation();
                                  openInExplorer(node.root).catch(() => {});
                                }}
                                variant="ghost"
                              >
                                <ExternalLink size={13} />
                              </IconButton>
                            ) : null}
                            {canDeleteWorkspace ? (
                              <IconButton
                                className="tree-action tree-delete"
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
                                          aria-label={runStatus === 'waiting_permission' ? '等待授权' : '会话运行中'}
                                          className={classNames('session-run-dot', `is-${runStatus}`)}
                                          data-testid="session-run-status"
                                          role="status"
                                        >
                                          {runStatus === 'waiting_permission' ? (
                                            <ShieldAlert aria-hidden="true" size={11} />
                                          ) : (
                                            <LoaderCircle aria-hidden="true" className="session-run-spinner" size={11} />
                                          )}
                                          <span>{runStatus === 'waiting_permission' ? '授权' : '运行中'}</span>
                                        </em>
                                      ) : null}
                                    </button>
                                    <IconButton
                                      className="tree-action tree-delete"
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
          </div>
        )}
      </div>
    </aside>
  );
}
