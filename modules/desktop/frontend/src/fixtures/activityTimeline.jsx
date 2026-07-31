import React from 'react';
import { createRoot } from 'react-dom/client';
import { RunActivityPanel } from '../components/RunActivityPanel.jsx';
import { normalizeRunEvent } from '../lib/activityEvents.js';
import '../styles/app.css';

const runId = 'run_fixture_1';

const events = [
  normalizeRunEvent({
    protocol_version: '2026-07-13', event_id: 'evt_tool',
    type: 'tool_started',
    run_id: runId,
    session_id: 'session_fixture', assignment_id: 'assignment-entry', run_seq: 1, worker_seq: 1,
    worker: { id: 'worker-01', profile_key: 'entry' },
    payload: {
      tool_name: 'workspace.read_file',
      status: 'running',
      input: { path: 'README.md' },
    },
  }),
  normalizeRunEvent({
    protocol_version: '2026-07-13', event_id: 'evt_message',
    type: 'message_delta',
    run_id: runId,
    session_id: 'session_fixture', assignment_id: 'assignment-planner', run_seq: 2, worker_seq: 1,
    worker: { id: 'worker-02', profile_key: 'planner' },
    payload: { delta: 'planner saw the file' },
  }),
  normalizeRunEvent({
    protocol_version: '2026-07-13', event_id: 'evt_permission',
    type: 'permission_required',
    run_id: runId,
    session_id: 'session_fixture', assignment_id: 'assignment-entry', run_seq: 3, worker_seq: 3,
    worker: { id: 'worker-01', profile_key: 'entry' },
    payload: {
      permission_id: 'perm_fixture',
      tool_name: 'shell.exec',
      summary: 'Allow shell command',
      risk: 'high',
    },
  }),
  normalizeRunEvent({
    protocol_version: '2026-07-13', event_id: 'evt_usage',
    type: 'usage',
    run_id: runId,
    session_id: 'session_fixture', assignment_id: 'assignment-entry', run_seq: 4, worker_seq: 4,
    worker: { id: 'worker-01', profile_key: 'entry' },
    payload: {
      input_tokens: 15028,
      output_tokens: 16,
      cache_read_tokens: 14912,
      cache_write_tokens: 0,
      cache_hit_ratio: 0.9923,
      cache_mode: 'implicit',
      cache_epoch: 'fixture-epoch',
    },
  }),
  normalizeRunEvent({
    protocol_version: '2026-07-13', event_id: 'evt_finish',
    type: 'finish',
    run_id: runId,
    session_id: 'session_fixture', assignment_id: 'assignment-entry', run_seq: 5, worker_seq: 5,
    worker: { id: 'worker-01', profile_key: 'entry' },
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
  lastRunSeq: 4,
  messageCount: 1,
  toolCount: 1,
  startedAt: '2026-07-09T00:00:00Z',
  updatedAt: '2026-07-09T00:00:01Z',
}];

const tools = [{
  id: 'tool_fixture',
  runId,
  name: 'workspace.read_file',
  displayName: 'Read file',
  status: 'completed',
  runSeq: 1,
}, {
  id: 'tool_fixture_2', runId, name: 'workspace.read_file', displayName: 'Read file', status: 'completed', runSeq: 2,
}, {
  id: 'tool_fixture_3', runId, name: 'shell.exec', displayName: 'Shell', status: 'failed', runSeq: 3,
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
