import { Bot, FolderOpen, GitFork, MessageSquarePlus, Minimize2 } from 'lucide-react';
import { Button } from './ui/button.jsx';

export function Sidebar({
  sessions,
  currentSessionId,
  workspace,
  onCompactSession,
  onForkSession,
  onNewSession,
  onSelectSession,
}) {
  const workspaceTitle = workspace?.name || workspace?.root_path || workspace?.root || '未打开工作区';
  const hasCurrentSession = Boolean(currentSessionId);

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

      <section className="sidebar-section">
        <div className="section-title">
          <FolderOpen size={12} />
          <span>工作区</span>
        </div>
        <div className="workspace-card" title={workspaceTitle}>
          <strong>{workspaceTitle}</strong>
        </div>
      </section>

      <section className="sidebar-section grow">
        <div className="section-title">
          <Bot size={12} />
          <span>会话</span>
        </div>
        <div className="session-list">
          {sessions.map((session) => (
            <button
              className={session.id === currentSessionId ? 'session-item active' : 'session-item'}
              data-testid="session-item"
              key={session.id}
              onClick={() => onSelectSession(session.id)}
              type="button"
            >
              <strong>{session.title}</strong>
            </button>
          ))}
        </div>
      </section>
    </aside>
  );
}
