import React from 'react';
import { createRoot } from 'react-dom/client';
import { ToolCallCard } from '../components/ToolCallCard.jsx';
import '../styles/app.css';

const tools = [
  {
    id: 'tool-read',
    name: 'workspace.read_file',
    displayName: 'Read file',
    arguments: { path: 'README.md' },
    output: 'README.md loaded',
    status: 'completed',
    durationMs: 184,
  },
  {
    id: 'tool-shell',
    name: 'shell.exec',
    displayName: 'Shell command',
    arguments: {
      command: "Get-Content -Raw 'C:\\Users\\User\\.agents\\skills\\code-1.0.4\\planning.md'",
    },
    output: 'No output',
    status: 'completed',
    durationMs: 624,
  },
  {
    id: 'tool-search',
    name: 'workspace.search',
    displayName: 'Search workspace',
    arguments: { pattern: 'tool-card|ToolCallCard|tool_call' },
    error: 'Search stopped before all files were scanned',
    status: 'failed',
    durationMs: 2400,
  },
  {
    // Running tools stay compact by default (one-line live row).
    id: 'tool-running',
    name: 'workspace.grep',
    displayName: 'Grep workspace',
    arguments: { pattern: 'ToolCallCard', path: 'modules/desktop' },
    status: 'running',
    startedAt: new Date(Date.now() - 4200).toISOString(),
  },
];

function ToolCardFixture() {
  const firstIndex = 54;
  const total = 70;
  return (
    <main style={{ margin: '48px auto', maxWidth: 760, padding: '0 20px' }}>
      <section className="conversation" style={{ padding: 0 }}>
        {tools.map((tool, index) => (
          <ToolCallCard
            callIndex={firstIndex + index}
            callTotal={total}
            item={tool}
            key={tool.id}
          />
        ))}
      </section>
    </main>
  );
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <ToolCardFixture />
  </React.StrictMode>,
);
