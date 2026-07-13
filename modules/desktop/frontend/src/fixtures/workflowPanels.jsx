import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WorkerPanel } from '../components/WorkerPanel.jsx';
import { ChatConversation } from '../components/chat/ChatConversation.jsx';
import '../styles/app.css';

const messages = [
  {
    id: 'message_user_restore',
    role: 'user',
    agent: 'user',
    runSeq: 1,
    text: 'Restore previous session state',
  },
  {
    id: 'message_assistant_restore',
    role: 'assistant',
    agent: 'worker',
    runSeq: 4,
    text: 'Restored messages, tools, permissions, and Worker assignments.',
  },
];

const tools = [
  {
    id: 'tool_completed',
    runId: 'run_restore',
    workerId: 'worker-01',
    assignmentId: 'assignment-entry',
    name: 'workspace.read_file',
    displayName: 'Read file',
    risk: 'low',
    arguments: { path: 'README.md' },
    status: 'completed',
    output: 'README.md loaded',
    runSeq: 2,
  },
  {
    id: 'tool_failed',
    runId: 'run_restore',
    workerId: 'worker-02',
    assignmentId: 'assignment-planner',
    name: 'shell.exec',
    displayName: 'Shell command',
    risk: 'high',
    arguments: { command: 'exit 1' },
    status: 'failed',
    error: 'exit status 1',
    runSeq: 3,
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
    workerId: 'worker-01',
    assignmentId: 'assignment-entry',
    runSeq: 5,
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
    workerId: 'worker-02',
    assignmentId: 'assignment-planner',
    runSeq: 6,
  },
];

const workers = [
  {
    id: 'worker-01', state: 'ready', healthy: true, currentAssignmentId: '',
  },
  {
    id: 'worker-02', state: 'busy', healthy: true, currentAssignmentId: 'assignment-planner', profileKey: 'goal-planner',
  },
];

const assignments = [
  { id: 'assignment-planner', runId: 'run_restore', workerId: 'worker-02', profileKey: 'goal-planner', status: 'running', task: 'Reading restored context', workerSeq: 3 },
  { id: 'assignment-archive', runId: 'run_restore', workerId: 'worker-01', profileKey: 'archivist', status: 'completed', summary: 'Finished', workerSeq: 2 },
];

function WorkflowFixture() {
  const [permissionItems, setPermissionItems] = useState(permissions);
  const [lastPermission, setLastPermission] = useState('none');
  const [lastAssignment, setLastAssignment] = useState('none');

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
          <WorkerPanel
            assignments={assignments}
            onCancelAssignment={(assignment) => setLastAssignment(`${assignment.id}:${assignment.status}`)}
            workers={workers}
          />
        </aside>
      </main>
      <footer>
        <span data-testid="permission-result">{lastPermission}</span>
        <span data-testid="worker-result">{lastAssignment}</span>
      </footer>
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <WorkflowFixture />
  </React.StrictMode>,
);
