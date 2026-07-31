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
    id: 'message_reasoning_restore',
    role: 'reasoning',
    agent: 'assistant',
    runSeq: 4,
    text: 'First inspect the restored run state, then verify the tool results.',
  },
  {
    // Root-facing assistant reply (main conversation). No worker/profile markers
    // so isMainConversationItem keeps it public and the UI labels it as 助手.
    id: 'message_assistant_restore',
    role: 'assistant',
    agent: 'assistant',
    runSeq: 5,
    text: 'Restored messages, tools, permissions, and Worker assignments.',
  },
  {
    id: 'message_worker_private',
    role: 'assistant',
    agent: 'worker-02',
    assignmentId: 'assignment-planner',
    workerId: 'worker-02',
    profileKey: 'planner',
    visibility: 'worker_private',
    runSeq: 7,
    text: 'Private planner analysis.',
  },
  {
    // Public collaborative Worker trail — only visible on the Worker tab.
    id: 'message_worker_public',
    role: 'assistant',
    agent: 'worker-02',
    assignmentId: 'assignment-planner',
    workerId: 'worker-02',
    profileKey: 'planner',
    runSeq: 8,
    text: 'Reading restored context',
  },
];

const tools = [
  {
    // Root tool call shown in the main conversation timeline.
    id: 'tool_completed',
    runId: 'run_restore',
    name: 'workspace.read_file',
    displayName: 'Read file',
    risk: 'low',
    arguments: { path: 'README.md' },
    status: 'completed',
    output: 'README.md loaded',
    runSeq: 2,
  },
  {
    id: 'tool_root_stats',
    runId: 'run_restore',
    name: 'workspace.stats',
    displayName: 'Workspace stats',
    risk: 'low',
    arguments: { path: '.' },
    status: 'completed',
    output: '39 files',
    runSeq: 3,
  },
  {
    // Worker-scoped tool: appears on the planner Worker tab only.
    id: 'tool_failed',
    runId: 'run_restore',
    workerId: 'worker-02',
    assignmentId: 'assignment-planner',
    profileKey: 'planner',
    name: 'shell.exec',
    displayName: 'Shell command',
    risk: 'high',
    arguments: { command: 'exit 1' },
    status: 'failed',
    error: 'exit status 1',
    output: JSON.stringify({
      schema: 'red_panda.tool_result.v1',
      tool: 'shell.exec',
      status: 'failed',
      ok: false,
      text: 'exit status 1',
      error: 'exit status 1',
      data: { raw: 'go: cannot find main module; see go help modules' },
      meta: {},
    }),
    runSeq: 3,
  },
];

const providerProfiles = [{
  id: 'provider_fixture',
  name: 'Work',
  model: 'model-fast',
  active: true,
  models: [
    { model: 'model-fast', label: 'Fast', maxTokens: 128000, reasoningEffort: 'low' },
    { model: 'model-deep', label: 'Deep', maxTokens: 200000, reasoningEffort: 'high' },
  ],
}];

const permissions = [
  {
    // Root-facing permission cards stay on the main timeline so approve/deny
    // can be exercised there. Worker tabs are read-only for decisions.
    id: 'perm_approve_restore',
    runId: 'run_restore',
    summary: 'Allow shell read',
    detail: 'Agent Runtime is waiting for a restored approval.',
    status: 'pending',
    toolName: 'shell.exec',
    risk: 'medium',
    arguments: { command: 'cat README.md' },
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
    runSeq: 6,
  },
];

const assignments = [
  { id: 'assignment-planner', runId: 'run_restore', workerId: 'worker-02', profileKey: 'planner', status: 'running', task: 'Reading restored context', workerSeq: 3 },
  { id: 'assignment-archive', runId: 'run_restore', workerId: 'worker-01', profileKey: 'archivist', status: 'completed', summary: 'Finished', workerSeq: 2 },
  ...Array.from({ length: 6 }, (_, index) => ({
    id: `assignment-history-${index + 1}`,
    runId: 'run_restore',
    workerId: `worker-history-${index + 1}`,
    profileKey: index % 2 === 0 ? 'verifier' : 'implementer',
    status: 'completed',
    summary: `Archived result ${index + 1}`,
    workerSeq: index + 4,
  })),
];

function WorkflowFixture() {
  const [permissionItems, setPermissionItems] = useState(permissions);
  const [lastPermission, setLastPermission] = useState('none');
  const [lastAssignment, setLastAssignment] = useState('none');
  const [conversationTabs, setConversationTabs] = useState([
    { id: 'main', kind: 'main', title: '主对话', closable: false },
  ]);
  const [activeConversationTab, setActiveConversationTab] = useState('main');
  const [providerSelection, setProviderSelection] = useState({
    providerProfileId: 'provider_fixture', model: 'model-fast', enableThinking: false, reasoningEffort: '',
  });

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
            messages={messages}
            model={providerSelection.model}
            enableThinking={providerSelection.enableThinking}
            onCloseConversationTab={closeConversationTab}
            onResolvePermission={resolvePermission}
            onProviderProfileChange={(providerProfileId, model) => setProviderSelection((current) => ({
              ...current, providerProfileId, model, reasoningEffort: '',
            }))}
            onReasoningEffortChange={(reasoningEffort) => setProviderSelection((current) => ({
              ...current, reasoningEffort,
            }))}
            onEnableThinkingChange={(enableThinking) => setProviderSelection((current) => ({
              ...current, enableThinking,
            }))}
            onSelectConversationTab={setActiveConversationTab}
            permissions={permissionItems}
            providerProfileId={providerSelection.providerProfileId}
            providerProfiles={providerProfiles}
            reasoningEffort={providerSelection.reasoningEffort}
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
