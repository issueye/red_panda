import { FileText, Folder, GitCompare, RefreshCw } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Button } from './ui/button.jsx';

function flattenTree(node, depth = 0, items = []) {
  if (!node) return items;
  items.push({ ...node, depth });
  for (const child of node.children || []) {
    flattenTree(child, depth + 1, items);
  }
  return items;
}

function displayContent(file, diff) {
  if (diff?.diff) return diff.diff;
  if (!file) return 'Select a file from the workspace tree.';
  if (file.binary) return 'Binary file preview is disabled.';
  return file.content || '';
}

export function WorkspacePanel({ apiJson, workspace }) {
  const [tree, setTree] = useState(null);
  const [selectedPath, setSelectedPath] = useState('');
  const [file, setFile] = useState(null);
  const [diff, setDiff] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const items = useMemo(() => flattenTree(tree).filter((item) => item.path !== ''), [tree]);

  async function loadTree() {
    if (!workspace) return;
    setLoading(true);
    setError('');
    try {
      const data = await apiJson('/api/v1/workspaces/tree?max_depth=3');
      setTree(data);
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  async function openFile(path) {
    if (!path) return;
    setSelectedPath(path);
    setDiff(null);
    setLoading(true);
    setError('');
    try {
      const data = await apiJson(`/api/v1/workspaces/file?path=${encodeURIComponent(path)}`);
      setFile(data);
    } catch (err) {
      setFile(null);
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  async function loadDiff() {
    setLoading(true);
    setError('');
    try {
      const query = selectedPath ? `?path=${encodeURIComponent(selectedPath)}` : '';
      const data = await apiJson(`/api/v1/workspaces/diff${query}`);
      setDiff(data);
      if (!data.available && data.reason) {
        setError(data.reason);
      }
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadTree();
  }, [workspace?.root_path, workspace?.root]);

  return (
    <section className="workspace-panel-content">
      <div className="panel-header">
        <div>
          <strong>Workspace</strong>
          <span>{workspace?.root_path || workspace?.root || 'No workspace open'}</span>
        </div>
        <Button icon={<RefreshCw size={14} />} onClick={loadTree} variant="ghost">
          Refresh
        </Button>
      </div>

      <div className="workspace-panel-grid">
        <div className="workspace-tree">
          {items.length === 0 ? <div className="workspace-empty">No files loaded</div> : null}
          {items.map((item) => {
            const isFile = item.type === 'file';
            const Icon = isFile ? FileText : Folder;
            return (
              <button
                className={item.path === selectedPath ? 'tree-row active' : 'tree-row'}
                disabled={!isFile}
                key={item.path}
                onClick={() => openFile(item.path)}
                style={{ paddingLeft: `${8 + item.depth * 12}px` }}
                title={item.path}
                type="button"
              >
                <Icon size={13} />
                <span>{item.name}</span>
              </button>
            );
          })}
        </div>

        <div className="workspace-preview">
          <div className="workspace-preview-head">
            <span>{selectedPath || 'Preview'}</span>
            <Button icon={<GitCompare size={14} />} onClick={loadDiff} variant="soft">
              Diff
            </Button>
          </div>
          {error ? <div className="workspace-error">{error}</div> : null}
          <pre>{loading ? 'Loading...' : displayContent(file, diff)}</pre>
        </div>
      </div>
    </section>
  );
}
