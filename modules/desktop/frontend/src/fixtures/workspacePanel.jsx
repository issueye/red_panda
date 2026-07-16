import React from 'react';
import { createRoot } from 'react-dom/client';
import { WorkspacePanel } from '../components/WorkspacePanel.jsx';
import '../styles/app.css';

const markdown = `# Workspace report

This preview renders **formatted Markdown** with an [external link](https://example.com).

- [x] Render headings and lists
- [ ] Verify the final layout

| Item | Status |
| --- | --- |
| Markdown | Ready |

\`\`\`js
const rendered = true;
\`\`\`

<script>window.fixtureUnsafeHtml = true</script>`;

async function apiJson(url) {
  if (url.startsWith('/api/v1/workspaces/tree')) {
    return {
      path: '',
      type: 'directory',
      children: [
        { name: 'report.md', path: 'report.md', type: 'file' },
        { name: 'notes.txt', path: 'notes.txt', type: 'file' },
        {
          name: 'docs',
          path: 'docs',
          type: 'directory',
          children: [
            { name: 'guide.md', path: 'docs/guide.md', type: 'file' },
          ],
        },
      ],
    };
  }
  if (url.includes('/api/v1/workspaces/file')) {
    const path = new URL(url, window.location.origin).searchParams.get('path');
    return { path, binary: false, content: path === 'report.md' ? markdown : '# Plain text' };
  }
  if (url.startsWith('/api/v1/workspaces/diff')) {
    return { available: true, diff: '@@ -1 +1 @@\n-# Old title\n+# New title' };
  }
  throw new Error(`Unexpected fixture request: ${url}`);
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <div className="workspace-fixture-shell">
      <WorkspacePanel apiJson={apiJson} workspace={{ root: 'C:\\fixture' }} />
    </div>
  </React.StrictMode>,
);
