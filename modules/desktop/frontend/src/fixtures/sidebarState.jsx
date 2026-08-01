import React from 'react';
import { createRoot } from 'react-dom/client';
import { Sidebar } from '../components/Sidebar.jsx';
import '../styles/app.css';

const workspace = {
  id: 'workspace-fixture',
  name: 'red_panda',
  root: 'E:/codes/ai_agent/red_panda',
};

const sessions = [
  { id: 'session-running', title: '正在分析 Worker 会话', workspaceRoot: workspace.root },
  { id: 'session-permission', title: '等待命令授权', workspaceRoot: workspace.root },
  { id: 'session-idle', title: '已完成的会话', workspaceRoot: workspace.root },
];

function SidebarStateFixture() {
  return (
    <div style={{ height: '100vh', width: 300 }}>
      <Sidebar
        currentSessionId="session-running"
        leftTab="sessions"
        onLeftTabChange={() => {}}
        onNewSession={() => {}}
        onOpenWorkspace={() => {}}
        onSelectSession={() => {}}
        onSelectWorkspace={() => {}}
        sessionRunStatus={{
          'session-running': 'running',
          'session-permission': 'waiting_permission',
          'session-idle': 'idle',
        }}
        sessions={sessions}
        workspace={workspace}
        workspaces={[workspace]}
      />
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <SidebarStateFixture />
  </React.StrictMode>,
);
