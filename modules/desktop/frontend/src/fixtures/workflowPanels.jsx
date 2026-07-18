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
    assignmentId: 'assignment-entry',
    workerId: 'worker-01',
    profileKey: 'entry',
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

const assignments = [
  { id: 'assignment-planner', runId: 'run_restore', workerId: 'worker-02', profileKey: 'goal-planner', status: 'running', task: 'Reading restored context', workerSeq: 3 },
  { id: 'assignment-archive', runId: 'run_restore', workerId: 'worker-01', profileKey: 'archivist', status: 'completed', summary: 'Finished', workerSeq: 2 },
  ...Array.from({ length: 6 }, (_, index) => ({
    id: `assignment-history-${index + 1}`,
    runId: 'run_restore',
    workerId: `worker-history-${index + 1}`,
    profileKey: index % 2 === 0 ? 'goal-verifier' : 'goal-implementer',
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
            goalExpanded={Boolean(goal)}
            goalSessionId="local-design"
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
          />
        </aside>
      </main>
      <footer>
        <span data-testid="permission-result">{lastPermission}</span>
        <span data-testid="worker-result">{lastAssignment}</span>
        <button data-testid="fixture-normal-mode" onClick={() => setGoal(null)} type="button">Normal</button>
        <button
          data-testid="fixture-goal-mode"
          onClick={() => setGoal({
            id: 'goal_fixture', title: '交付目标驱动执行', objective: '让 Goal 根据结果差距持续选择行动，直到证据证明目标达成',
            status: 'active', strategy: '先验证控制器闭环，再补齐界面与回归测试',
            currentActionId: 'action-ui', currentAction: '验证 outcome dashboard 的信息层级',
            criteria: [
              { id: 'controller', description: '控制器能根据 evidence 选择下一行动', status: 'met', evidence: 'Gateway V2 feedback-loop tests passed' },
              { id: 'runtime', description: 'Runtime 不依赖固定 pipeline phase', status: 'met', evidence: 'Agent full test suite passed' },
              { id: 'desktop', description: 'Desktop 清楚展示标准、行动和剩余差距', status: 'not_met', evidence: '等待视觉验收' },
            ],
            actions: [
              { id: 'action-api', key: 'api', title: '重构 Goal 控制器 API', status: 'done', acceptance: 'V2 tools and persistence pass tests', sortOrder: 0 },
              { id: 'action-ui', key: 'ui', title: '验证 outcome dashboard', status: 'active', acceptance: 'Desktop layout is readable on desktop and mobile', sortOrder: 1 },
              { id: 'action-e2e', key: 'e2e', title: '完成端到端回归', status: 'queued', acceptance: 'All module and UI tests pass', sortOrder: 2 },
            ],
            lastAssessment: {
              verdict: 'progress', summary: '后端和 Runtime 已完成，界面仍需视觉验收',
              gap: '确认窄屏无溢出且评估证据易扫描', decision: '完成 Desktop 截图与交互检查',
            },
            iteration: 3, maxIterations: 20, stagnationCount: 0, maxStagnation: 3,
            usedToolTurns: 31, maxTotalToolTurns: 96, usedWallTimeSec: 210, maxWallTimeSec: 1800,
          })}
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
