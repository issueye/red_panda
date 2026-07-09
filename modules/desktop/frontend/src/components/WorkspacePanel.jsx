import { FileText, Folder, GitCompare, RefreshCw } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Button } from './ui/button.jsx';
import { ErrorMessage, InlineEmpty } from './ui/feedback.jsx';
import { PanelHeader } from './ui/panel.jsx';

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
  if (!file) return '请从工作区树中选择文件。';
  if (file.binary) return '二进制文件暂不支持预览。';
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
      <PanelHeader
        action={(
          <Button icon={<RefreshCw size={14} />} onClick={loadTree} variant="ghost">
            刷新
          </Button>
        )}
        title="工作区"
      />

      <div className="workspace-panel-grid">
        <div className="workspace-tree">
          {items.length === 0 ? <InlineEmpty as="div" className="workspace-empty">暂无文件</InlineEmpty> : null}
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
            <span>{selectedPath || '预览'}</span>
            <Button icon={<GitCompare size={14} />} onClick={loadDiff} variant="soft">
              差异
            </Button>
          </div>
          <ErrorMessage as="div" className="workspace-error">{error}</ErrorMessage>
          <pre>{loading ? '加载中...' : displayContent(file, diff)}</pre>
        </div>
      </div>
    </section>
  );
}
