import React, { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryPanel } from '../components/MemoryPanel.jsx';
import '../styles/app.css';

const initialMemory = [
  {
    id: 'mem_project',
    scope: 'project',
    kind: 'fact',
    status: 'active',
    title: 'Project command',
    content: 'Use npm test for frontend checks.',
    confidence: 'high',
    workspace_root: 'D:/workspace',
    source: 'user',
    updated_at: '2026-07-09T10:00:00Z',
  },
  {
    id: 'mem_session',
    scope: 'session',
    kind: 'decision',
    status: 'active',
    title: 'Session decision',
    content: 'Keep the Memory panel compact.',
    confidence: 'medium',
    session_id: 'session_memory_fixture',
    source: 'user',
    updated_at: '2026-07-09T10:01:00Z',
  },
  {
    id: 'mem_disabled',
    scope: 'session',
    kind: 'warning',
    status: 'disabled',
    title: 'Disabled note',
    content: 'Do not inject this disabled memory.',
    confidence: 'low',
    session_id: 'session_memory_fixture',
    source: 'user',
    updated_at: '2026-07-09T10:02:00Z',
  },
];

function MemoryFixture() {
  const [records, setRecords] = useState(initialMemory);
  const [lastCall, setLastCall] = useState('none');

  async function apiJson(path, options = {}) {
    setLastCall(`${options.method || 'GET'} ${path}`);
    const method = options.method || 'GET';
    const body = options.body ? JSON.parse(options.body) : null;
    if (method === 'GET' && path.startsWith('/api/v1/memory')) {
      const url = new URL(path, 'http://fixture.local');
      const scope = url.searchParams.get('scope');
      const status = url.searchParams.get('status') || 'active';
      const workspaceRoot = url.searchParams.get('workspace_root');
      const sessionId = url.searchParams.get('session_id');
      return records.filter((item) => (
        (!scope || item.scope === scope) &&
        (status === 'all' || item.status === status) &&
        (!workspaceRoot || item.workspace_root === workspaceRoot) &&
        (!sessionId || item.session_id === sessionId)
      ));
    }
    if (method === 'POST' && path === '/api/v1/memory') {
      const created = {
        id: `mem_created_${records.length}`,
        status: 'active',
        source: 'user',
        updated_at: new Date().toISOString(),
        ...body,
      };
      setRecords((items) => [created, ...items]);
      return created;
    }
    if (method === 'PUT' && path.startsWith('/api/v1/memory/')) {
      const id = path.split('/').pop();
      const current = records.find((item) => item.id === id);
      const updated = { ...current, ...body, updated_at: new Date().toISOString() };
      setRecords((items) => items.map((item) => (item.id === id ? updated : item)));
      return updated;
    }
    if (method === 'DELETE' && path.startsWith('/api/v1/memory/')) {
      const id = path.split('/').pop();
      const current = records.find((item) => item.id === id);
      const deleted = { ...current, status: 'deleted', deleted_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      setRecords((items) => items.map((item) => (item.id === id ? deleted : item)));
      return deleted;
    }
    if (method === 'POST' && path === '/api/v1/memory/preview-run') {
      return {
        items: records.filter((item) => item.status === 'active' && (
          (item.scope === 'project' && item.workspace_root === body.workspace_root) ||
          (item.scope === 'session' && item.session_id === body.session_id)
        )),
        context: 'Memory:\n- [session/decision] Session decision: Keep the Memory panel compact.\n- [project/fact] Project command: Use npm test for frontend checks.',
      };
    }
    throw new Error(`unhandled fixture request ${method} ${path}`);
  }

  return (
    <div className="activity-fixture-shell">
      <aside className="right-panel">
        <MemoryPanel
          apiJson={apiJson}
          currentSessionId="session_memory_fixture"
          workspaceRoot="D:/workspace"
        />
      </aside>
      <span data-testid="memory-last-call">{lastCall}</span>
    </div>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <MemoryFixture />
  </React.StrictMode>,
);
