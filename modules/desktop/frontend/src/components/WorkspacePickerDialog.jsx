import { FolderOpen, FolderSearch } from 'lucide-react';
import { useEffect, useState } from 'react';
import { workspaceDisplayName } from '../lib/sessionTree.js';
import { Button } from './ui/button.jsx';
import { Dialog } from './ui/dialog.jsx';

/**
 * 选择工作区：最近列表 + 路径输入 + 可选系统目录浏览。
 */
export function WorkspacePickerDialog({
  open,
  workspaces = [],
  currentRoot = '',
  onClose,
  onBrowse,
  onOpen,
}) {
  const [path, setPath] = useState(currentRoot || '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (open) {
      setPath(currentRoot || '');
      setError('');
      setBusy(false);
    }
  }, [open, currentRoot]);

  async function submit(root) {
    const next = String(root || path || '').trim();
    if (!next) {
      setError('请输入或选择工作区路径。');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await onOpen?.(next);
      onClose?.();
    } catch (err) {
      setError(err?.message || '打开工作区失败');
    } finally {
      setBusy(false);
    }
  }

  async function browse() {
    setError('');
    try {
      const selected = await onBrowse?.();
      if (selected) {
        setPath(selected);
      }
    } catch (err) {
      setError(err?.message || '无法打开目录选择器');
    }
  }

  if (!open) return null;

  return (
    <Dialog
      description="选择最近工作区，或输入本地目录路径。"
      footer={(
        <>
          <Button disabled={busy} onClick={onClose} variant="ghost">取消</Button>
          <Button
            data-testid="workspace-picker-open"
            disabled={busy || !path.trim()}
            onClick={() => submit(path)}
          >
            {busy ? '打开中…' : '打开工作区'}
          </Button>
        </>
      )}
      onClose={onClose}
      size="md"
      testId="workspace-picker"
      title="选择工作区"
    >
      <div className="workspace-picker">
        {workspaces.length > 0 ? (
          <section className="workspace-picker-recent">
            <div className="section-title">
              <FolderOpen size={12} />
              <span>最近使用</span>
            </div>
            <div className="workspace-picker-list">
              {workspaces.map((item) => {
                const root = item.root_path || item.root || '';
                const active = currentRoot
                  && root.replace(/\\/g, '/').toLowerCase() === currentRoot.replace(/\\/g, '/').toLowerCase();
                return (
                  <button
                    className={active ? 'workspace-picker-item active' : 'workspace-picker-item'}
                    data-testid="workspace-picker-item"
                    disabled={busy}
                    key={item.id || root}
                    onClick={() => submit(root)}
                    title={root}
                    type="button"
                  >
                    <strong>{item.name || workspaceDisplayName(root)}</strong>
                    <span>{root}</span>
                  </button>
                );
              })}
            </div>
          </section>
        ) : null}

        <label className="ui-dialog-field">
          <span>工作区路径</span>
          <div className="workspace-picker-path-row">
            <input
              data-testid="workspace-picker-input"
              disabled={busy}
              onChange={(event) => setPath(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault();
                  submit(path);
                }
              }}
              placeholder="例如 E:\code\my-project"
              value={path}
            />
            <Button
              data-testid="workspace-picker-browse"
              disabled={busy}
              icon={<FolderSearch size={14} />}
              onClick={browse}
              type="button"
              variant="soft"
            >
              浏览
            </Button>
          </div>
        </label>
        {error ? <p className="ui-error-message">{error}</p> : null}
      </div>
    </Dialog>
  );
}
