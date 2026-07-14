import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WorkerPanel } from '../components/WorkerPanel.jsx';
import { ChatPanel } from '../components/chat/ChatPanel.jsx';
import { displayStatus, displayWorkerProfileName } from '../lib/displayLabels.js';
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
  {
    id: 'message_worker_private',
    role: 'assistant',
    agent: 'worker-02',
    assignmentId: 'assignment-planner',
    workerId: 'worker-02',
    visibility: 'worker_private',
    runSeq: 7,
    text: 'Private planner analysis.',
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
  const [conversationTabs, setConversationTabs] = useState([
    { id: 'main', kind: 'main', title: '主对话', closable: false },
  ]);
  const [activeConversationTab, setActiveConversationTab] = useState('main');
  const [goal, setGoal] = useState(null);

  function openAssignment(assignment) {
    const tabId = `worker:${assignment.id}`;
    const tab = {
      id: tabId,
      kind: 'worker',
      assignmentId: assignment.id,
      workerId: assignment.workerId,
      runId: assignment.runId,
      task: assignment.task,
      title: displayWorkerProfileName(assignment.profileKey || assignment.workerId),
      status: assignment.status,
      statusLabel: displayStatus(assignment.status),
      closable: true,
    };
    setConversationTabs((items) => (
      items.some((item) => item.id === tabId)
        ? items.map((item) => (item.id === tabId ? tab : item))
        : [...items, tab]
    ));
    setActiveConversationTab(tabId);
  }

  function closeConversationTab(tabId) {
    setConversationTabs((items) => items.filter((item) => item.id !== tabId));
    setActiveConversationTab((current) => (current === tabId ? 'main' : current));
  }

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
          <ChatPanel
            activeConversationTab={activeConversationTab}
            conversationTabs={conversationTabs}
            draft=""
            goal={goal}
            messages={messages}
            onCloseConversationTab={closeConversationTab}
            onResolvePermission={resolvePermission}
            onSelectConversationTab={setActiveConversationTab}
            permissions={permissionItems}
            showComposer
            todos={[{
              id: 'todo_restore', content: 'Verify restored workflow', status: 'in_progress',
            }]}
            todoOpenCount={1}
            tools={tools}
          />
        </section>
        <aside className="right-panel">
          <WorkerPanel
            assignments={assignments}
            onCancelAssignment={(assignment) => setLastAssignment(`${assignment.id}:${assignment.status}`)}
            onOpenAssignment={openAssignment}
            workers={workers}
          />
        </aside>
      </main>
      <footer>
        <span data-testid="permission-result">{lastPermission}</span>
        <span data-testid="worker-result">{lastAssignment}</span>
        <button data-testid="fixture-normal-mode" onClick={() => setGoal(null)} type="button">Normal</button>
        <button
          data-testid="fixture-goal-mode"
          onClick={() => setGoal({ id: 'goal_fixture', objective: 'Verify Goal mode', status: 'running' })}
          type="button"
        >Goal</button>
      </footer>
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <WorkflowFixture />
  </React.StrictMode>,
);
