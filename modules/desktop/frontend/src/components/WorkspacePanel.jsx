import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
  FileText,
  Folder,
  GitCompare,
  Maximize2,
  RefreshCw,
  X,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { openInExplorer } from '../lib/desktopShell.js';
import { Button } from './ui/button.jsx';
import { ErrorMessage, InlineEmpty } from './ui/feedback.jsx';
import { Markdown } from './ui/Markdown.jsx';
import { PanelHeader } from './ui/panel.jsx';

export function flattenVisibleTree(node, collapsedPaths = new Set(), depth = 0, items = []) {
  if (!node) return items;
  items.push({ ...node, depth });
  if (node.type === 'directory' && collapsedPaths.has(node.path)) return items;
  for (const child of node.children || []) {
    flattenVisibleTree(child, collapsedPaths, depth + 1, items);
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
  const [collapsedPaths, setCollapsedPaths] = useState(() => new Set());

  const items = useMemo(
    () => flattenVisibleTree(tree, collapsedPaths).filter((item) => item.path !== ''),
    [collapsedPaths, tree],
  );

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
    setCollapsedPaths(new Set());
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

  function toggleDirectory(path) {
    setCollapsedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
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
                aria-label={expanded ? '关闭工作区' : '独立查看工作区'}
                title={expanded ? '关闭工作区' : '独立查看工作区'}
                aria-pressed={expanded}
                data-testid="workspace-panel-layout-toggle"
                icon={expanded ? <X size={15} /> : <Maximize2 size={14} />}
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
            const isDirectory = item.type === 'directory';
            const isExpanded = isDirectory && !collapsedPaths.has(item.path);
            const Icon = isFile ? FileText : Folder;
            return (
              <button
                aria-expanded={isDirectory ? isExpanded : undefined}
                className={item.path === selectedPath ? 'tree-row active' : 'tree-row'}
                data-path={item.path}
                data-testid="workspace-tree-row"
                key={item.path}
                onClick={() => (isDirectory ? toggleDirectory(item.path) : openFile(item.path))}
                style={{ paddingLeft: `${8 + item.depth * 12}px` }}
                title={item.path}
                type="button"
              >
                <span aria-hidden="true" className="tree-row-disclosure">
                  {isDirectory ? (isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />) : null}
                </span>
                <Icon aria-hidden="true" size={13} />
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
            <Markdown className="workspace-markdown-preview" workspaceRoot={workspaceRoot}>{previewContent}</Markdown>
          ) : (
            <pre className="workspace-preview-source">{previewContent}</pre>
          )}
        </div>
      </div>
    </section>
  );
}
