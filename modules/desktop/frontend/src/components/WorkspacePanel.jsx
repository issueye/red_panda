import {
  ExternalLink,
  FileText,
  Folder,
  GitCompare,
  Maximize2,
  PanelRightClose,
  RefreshCw,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { openInExplorer } from '../lib/desktopShell.js';
import { Button } from './ui/button.jsx';
import { ErrorMessage, InlineEmpty } from './ui/feedback.jsx';
import { Markdown } from './ui/Markdown.jsx';
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

export function isMarkdownPath(path) {
  return /\.(?:md|markdown)$/i.test(String(path || ''));
}

export function WorkspacePanel({ apiJson, canFloat = false, expanded = false, onExpandedChange, workspace }) {
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

  const workspaceRoot = workspace?.root_path || workspace?.root || '';

  function resolveAbsolutePath(relativeOrAbsolute) {
    const target = String(relativeOrAbsolute || '').trim();
    if (!target) return workspaceRoot;
    if (/^[a-zA-Z]:[\\/]/.test(target) || target.startsWith('\\\\') || target.startsWith('/')) {
      return target;
    }
    if (!workspaceRoot) return target;
    const sep = workspaceRoot.includes('\\') ? '\\' : '/';
    return `${workspaceRoot.replace(/[\\/]+$/, '')}${sep}${target.replace(/^[\\/]+/, '')}`;
  }

  async function revealInExplorer(pathHint) {
    const absolute = resolveAbsolutePath(pathHint || workspaceRoot);
    if (!absolute) return;
    await openInExplorer(absolute);
  }

  const previewContent = loading ? '加载中...' : displayContent(file, diff);
  const showMarkdown = !loading && !diff?.diff && !file?.binary && isMarkdownPath(selectedPath);

  return (
    <section className="workspace-panel-content">
      <PanelHeader
        title="工作区"
        action={(
          <div className="workspace-panel-actions">
            {canFloat ? (
              <Button
                title={expanded ? '收回右侧' : '展开'}
                aria-pressed={expanded}
                data-testid="workspace-panel-layout-toggle"
                icon={expanded ? <PanelRightClose size={14} /> : <Maximize2 size={14} />}
                onClick={() => onExpandedChange?.(!expanded)}
                variant="soft"
              />
            ) : null}
            <Button
              title="资源管理器"
              data-testid="workspace-panel-open-explorer"
              disabled={!workspaceRoot}
              icon={<ExternalLink size={14} />}
              onClick={() => revealInExplorer(workspaceRoot)}
              variant="ghost"
            />
            <Button
              title="刷新"
              icon={<RefreshCw size={14} />}
              onClick={loadTree}
              variant="ghost"
            />
          </div>
        )}
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
            <div className="workspace-preview-actions">
              <Button
                title="打开位置"
                data-testid="workspace-file-open-explorer"
                disabled={!selectedPath && !workspaceRoot}
                icon={<ExternalLink size={14} />}
                onClick={() => revealInExplorer(selectedPath || workspaceRoot)}
                variant="ghost"
              />
              <Button
                title="差异"
                icon={<GitCompare size={14} />} onClick={loadDiff} variant="soft"
              />
            </div>
          </div>
          <ErrorMessage as="div" className="workspace-error">{error}</ErrorMessage>
          {showMarkdown ? (
            <Markdown className="workspace-markdown-preview">{previewContent}</Markdown>
          ) : (
            <pre className="workspace-preview-source">{previewContent}</pre>
          )}
        </div>
      </div>
    </section>
  );
}
