import React from 'react';
import { createRoot } from 'react-dom/client';
import { RunActivityPanel } from '../components/RunActivityPanel.jsx';
import { normalizeRunEvent } from '../lib/activityEvents.js';
import '../styles/app.css';

const runId = 'run_fixture_1';

const events = [
  normalizeRunEvent({
    id: 'evt_tool',
    type: 'tool_started',
    root_run_id: runId,
    run_id: runId,
    root_seq: 1,
    agent_seq: 1,
    agent_role: 'root',
    agent_name: 'root',
    payload: {
      tool_name: 'workspace.read_file',
      status: 'running',
      input: { path: 'README.md' },
    },
  }),
  normalizeRunEvent({
    id: 'evt_message',
    type: 'message_delta',
    root_run_id: runId,
    run_id: `${runId}:planner`,
    parent_run_id: runId,
    root_seq: 2,
    agent_seq: 1,
    agent_role: 'subagent',
    agent_name: 'planner',
    payload: { delta: 'planner saw the file' },
  }),
  normalizeRunEvent({
    id: 'evt_permission',
    type: 'permission_required',
    root_run_id: runId,
    run_id: runId,
    root_seq: 3,
    agent_seq: 3,
    agent_role: 'root',
    agent_name: 'root',
    payload: {
      permission_id: 'perm_fixture',
      tool_name: 'shell.exec',
      summary: 'Allow shell command',
      risk: 'high',
    },
  }),
  normalizeRunEvent({
    id: 'evt_finish',
    type: 'finish',
    root_run_id: runId,
    run_id: runId,
    root_seq: 4,
    agent_seq: 4,
    agent_role: 'root',
    agent_name: 'root',
    payload: { status: 'completed' },
  }),
];

const runs = [{
  id: runId,
  sessionId: 'session_fixture',
  runtimeMode: 'single_core',
  status: 'completed',
  input: 'fixture run',
  lastEventType: 'finish',
  lastRootSeq: 4,
  messageCount: 1,
  toolCount: 1,
  startedAt: '2026-07-09T00:00:00Z',
  updatedAt: '2026-07-09T00:00:01Z',
}];

const tools = [{
  id: 'tool_fixture',
  rootRunId: runId,
  name: 'workspace.read_file',
  displayName: 'Read file',
  status: 'completed',
  rootSeq: 1,
}];

const permissions = [{
  id: 'perm_fixture',
  runId,
  status: 'pending',
  summary: 'Allow shell command',
  toolName: 'shell.exec',
}];

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <div className="app-shell activity-fixture-shell">
      <aside className="right-panel">
        <RunActivityPanel
          currentRunId={runId}
          globalPendingPermissions={permissions}
          onLoadRunEvents={() => {}}
          onResolvePermission={() => {}}
          permissions={permissions}
          runEventsByRun={{ [runId]: events }}
          runEventsError={{}}
          runEventsLoading={{}}
          runs={runs}
          tools={tools}
        />
      </aside>
    </div>
  </React.StrictMode>,
);
