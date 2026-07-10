import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { SubAgentPanel } from '../components/SubAgentPanel.jsx';
import { ChatConversation } from '../components/chat/ChatConversation.jsx';
import '../styles/app.css';

const messages = [
  {
    id: 'message_user_restore',
    role: 'user',
    agent: 'user',
    rootSeq: 1,
    text: 'Restore previous session state',
  },
  {
    id: 'message_assistant_restore',
    role: 'assistant',
    agent: 'root',
    rootSeq: 4,
    text: 'Restored messages, tools, permissions, and subagents.',
  },
];

const tools = [
  {
    id: 'tool_completed',
    rootRunId: 'run_restore',
    name: 'workspace.read_file',
    displayName: 'Read file',
    risk: 'low',
    arguments: { path: 'README.md' },
    status: 'completed',
    output: 'README.md loaded',
    rootSeq: 2,
  },
  {
    id: 'tool_failed',
    rootRunId: 'run_restore',
    name: 'shell.exec',
    displayName: 'Shell command',
    risk: 'high',
    arguments: { command: 'exit 1' },
    status: 'failed',
    error: 'exit status 1',
    rootSeq: 3,
  },
];

const permissions = [
  {
    id: 'perm_approve_restore',
    runId: 'run_restore',
    summary: 'Allow shell read',
    detail: 'Agent Runtime is waiting for a restored approval.',
    status: 'pending',
    toolName: 'shell.exec',
    risk: 'medium',
    arguments: { command: 'cat README.md' },
    agent: '主代理',
    rootSeq: 5,
  },
  {
    id: 'perm_deny_restore',
    runId: 'run_restore',
    summary: 'Allow destructive shell command',
    detail: 'Agent Runtime wants to remove a generated file.',
    status: 'pending',
    toolName: 'shell.exec',
    risk: 'high',
    arguments: { command: 'rm generated.tmp' },
    agent: '子代理 planner',
    rootSeq: 6,
  },
];

const agents = [
  {
    id: 'root',
    name: 'root',
    role: 'root',
    status: 'idle',
    seq: 4,
  },
  {
    id: 'planner',
    name: 'planner',
    role: 'subagent',
    status: 'running',
    backend: 'process_pool',
    rootRunId: 'run_restore',
    runId: 'run_restore:planner',
    summary: 'Reading restored context',
    seq: 3,
  },
  {
    id: 'archivist',
    name: 'archivist',
    role: 'subagent',
    status: 'completed',
    backend: 'runtime_process',
    rootRunId: 'run_restore',
    runId: 'run_restore:archivist',
    summary: 'Finished',
    seq: 2,
  },
];

function WorkflowFixture() {
  const [permissionItems, setPermissionItems] = useState(permissions);
  const [lastPermission, setLastPermission] = useState('none');
  const [lastSubagent, setLastSubagent] = useState('none');

  function resolvePermission(id, decision) {
    setPermissionItems((items) => items.map((item) => (
      item.id === id
        ? { ...item, status: 'resolved', decision }
        : item
    )));
    setLastPermission(`${id}:${decision}`);
  }

  return (
    <div className="workflow-fixture-shell">
      <main>
        <section>
          <div className="panel-header">
            <div>
              <strong>Restored Conversation</strong>
              <span>Messages, tool cards, and pending approvals</span>
            </div>
          </div>
          <ChatConversation
            messages={messages}
            onResolvePermission={resolvePermission}
            permissions={permissionItems}
            tools={tools}
          />
        </section>
        <aside className="right-panel">
          <SubAgentPanel
            agents={agents}
            onCancelSubAgent={(agent) => setLastSubagent(`${agent.id}:${agent.status}`)}
          />
        </aside>
      </main>
      <footer>
        <span data-testid="permission-result">{lastPermission}</span>
        <span data-testid="subagent-result">{lastSubagent}</span>
      </footer>
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <WorkflowFixture />
  </React.StrictMode>,
);
