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
  const workspaceTitle = workspace?.name || workspace?.root_path || workspace?.root || 'No workspace';
  const workspaceDetail = workspace?.root_path || workspace?.root || 'Open one through the gateway API';
  const hasCurrentSession = Boolean(currentSessionId);

  return (
    <aside className="sidebar">
      <Button className="sidebar-primary" icon={<MessageSquarePlus size={15} />} onClick={onNewSession} variant="soft">
        New chat
      </Button>
      <div className="session-command-row">
        <Button data-testid="session-fork" disabled={!hasCurrentSession} icon={<GitFork size={14} />} onClick={onForkSession} variant="ghost">
          Fork
        </Button>
        <Button data-testid="session-compact" disabled={!hasCurrentSession} icon={<Minimize2 size={14} />} onClick={onCompactSession} variant="ghost">
          Compact
        </Button>
      </div>

      <section className="sidebar-section">
        <div className="section-title">
          <FolderOpen size={14} />
          <span>Workspace</span>
        </div>
        <div className="workspace-card">
          <strong>{workspaceTitle}</strong>
          <span>{workspaceDetail}</span>
        </div>
      </section>

      <section className="sidebar-section grow">
        <div className="section-title">
          <Bot size={14} />
          <span>Sessions</span>
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
              <span>{session.subtitle}</span>
            </button>
          ))}
        </div>
      </section>
    </aside>
  );
}
