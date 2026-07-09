import { Database, Eye, Plus, RefreshCw, Save, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  displayConfidence,
  displayMemoryKind,
  displayMemoryScope,
} from '../lib/displayLabels.js';
import {
  emptyMemoryDraft,
  memoryCreatePayload,
  memoryDraftFrom,
  memoryListQuery,
  memoryPreviewPayload,
  memoryUpdatePayload,
  normalizeMemoryRecord,
} from '../lib/memory.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button, IconButton } from './ui/button.jsx';
import { ErrorMessage, InlineEmpty } from './ui/feedback.jsx';
import { Field } from './ui/field.jsx';
import { PanelHeader } from './ui/panel.jsx';
import { SelectMenu } from './ui/select.jsx';

function compactTime(value) {
  if (!value) return '未记录';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '未记录';
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function uniqueById(items) {
  const seen = new Set();
  return items.filter((item) => {
    if (!item.id || seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
}

export function MemoryPanel({ apiJson, currentSessionId, workspaceRoot }) {
  const [items, setItems] = useState([]);
  const [scopeFilter, setScopeFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('active');
  const [selectedId, setSelectedId] = useState('');
  const [draft, setDraft] = useState(emptyMemoryDraft);
  const [preview, setPreview] = useState(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const selected = useMemo(() => items.find((item) => item.id === selectedId) || null, [items, selectedId]);

  async function loadMemory() {
    setLoading(true);
    setError('');
    try {
      const scopes = scopeFilter === 'all' ? ['project', 'session'] : [scopeFilter];
      const requests = scopes
        .filter((scope) => (scope === 'project' ? workspaceRoot : currentSessionId))
        .map((scope) => apiJson(memoryListQuery({
          scope,
          status: statusFilter,
          workspaceRoot,
          sessionId: currentSessionId,
        })).catch(() => []));
      const results = await Promise.all(requests);
      const normalized = uniqueById(results.flat().map(normalizeMemoryRecord))
        .sort((left, right) => new Date(right.updatedAt || 0) - new Date(left.updatedAt || 0));
      setItems(normalized);
      if (selectedId && !normalized.some((item) => item.id === selectedId)) {
        clearSelection();
      }
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  function clearSelection() {
    setSelectedId('');
    setDraft(emptyMemoryDraft);
  }

  function selectMemory(item) {
    setSelectedId(item.id);
    setDraft(memoryDraftFrom(item));
  }

  async function saveMemory() {
    setSaving(true);
    setError('');
    try {
      const saved = selected
        ? await apiJson(`/api/v1/memory/${encodeURIComponent(selected.id)}`, {
            method: 'PUT',
            body: JSON.stringify(memoryUpdatePayload(draft)),
          })
        : await apiJson('/api/v1/memory', {
            method: 'POST',
            body: JSON.stringify(memoryCreatePayload(draft, { workspaceRoot, sessionId: currentSessionId })),
          });
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) => [normalized, ...current.filter((item) => item.id !== normalized.id)]);
      setSelectedId(normalized.id);
      setDraft(memoryDraftFrom(normalized));
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setSaving(false);
    }
  }

  async function disableMemory() {
    if (!selected) return;
    setDraft((current) => ({ ...current, status: 'disabled' }));
    setSaving(true);
    setError('');
    try {
      const saved = await apiJson(`/api/v1/memory/${encodeURIComponent(selected.id)}`, {
        method: 'PUT',
        body: JSON.stringify({ status: 'disabled' }),
      });
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) => current
        .map((item) => (item.id === normalized.id ? normalized : item))
        .filter((item) => statusFilter === 'all' || item.status === statusFilter));
      setSelectedId(normalized.id);
      setDraft(memoryDraftFrom(normalized));
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setSaving(false);
    }
  }

  async function deleteMemory() {
    if (!selected) return;
    setSaving(true);
    setError('');
    try {
      const saved = await apiJson(`/api/v1/memory/${encodeURIComponent(selected.id)}`, { method: 'DELETE' });
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) => current
        .map((item) => (item.id === normalized.id ? normalized : item))
        .filter((item) => statusFilter === 'all' || item.status === statusFilter));
      setSelectedId(normalized.id);
      setDraft(memoryDraftFrom(normalized));
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setSaving(false);
    }
  }

  async function previewMemory() {
    setLoading(true);
    setError('');
    try {
      const data = await apiJson('/api/v1/memory/preview-run', {
        method: 'POST',
        body: JSON.stringify(memoryPreviewPayload({
          sessionId: currentSessionId,
          workspaceRoot,
          input: '',
        })),
      });
      setPreview({
        items: Array.isArray(data.items) ? data.items.map(normalizeMemoryRecord) : [],
        context: data.context || '',
      });
    } catch (err) {
      setPreview(null);
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadMemory();
  }, [currentSessionId, workspaceRoot, scopeFilter, statusFilter]);

  return (
    <section className="memory-panel-content" data-testid="memory-panel">
      <PanelHeader
        action={(
          <IconButton label="刷新记忆" onClick={loadMemory}>
            <RefreshCw size={14} />
          </IconButton>
        )}
        title="记忆"
      />

      <div className="memory-toolbar">
        <SelectMenu
          aria-label="记忆范围筛选"
          ariaLabel="记忆范围筛选"
          onChange={setScopeFilter}
          options={[
            ['all', '全部'],
            ['project', '项目'],
            ['session', '会话'],
          ]}
          value={scopeFilter}
        />
        <SelectMenu
          aria-label="记忆状态筛选"
          ariaLabel="记忆状态筛选"
          onChange={setStatusFilter}
          options={[
            ['active', '启用'],
            ['disabled', '停用'],
            ['deleted', '已删除'],
            ['all', '全部'],
          ]}
          value={statusFilter}
        />
      </div>

      <div className="memory-list" data-testid="memory-list">
        {items.length === 0 ? <InlineEmpty className="activity-empty">{loading ? '正在加载记忆' : '暂无记忆记录'}</InlineEmpty> : null}
        {items.map((item) => (
          <button
            className={item.id === selectedId ? 'memory-item active' : 'memory-item'}
            data-testid="memory-item"
            key={item.id}
            onClick={() => selectMemory(item)}
            type="button"
          >
            <Database size={14} />
            <span>
              <strong>{item.title}</strong>
              <em>{displayMemoryScope(item.scope)} / {displayMemoryKind(item.kind)} / {displayConfidence(item.confidence)}</em>
            </span>
            <StatusBadge className={`memory-status memory-status-${item.status}`} status={item.status} />
          </button>
        ))}
      </div>

      <div className="memory-editor">
        <div className="memory-editor-head">
          <strong>{selected ? '编辑记忆' : '新建记忆'}</strong>
          <Button icon={<Plus size={14} />} onClick={clearSelection} variant="ghost">
            新建
          </Button>
        </div>
        <div className="memory-editor-grid">
          <label>
            <span>范围</span>
            <SelectMenu
              ariaLabel="记忆范围"
              disabled={Boolean(selected)}
              onChange={(nextValue) => setDraft((current) => ({ ...current, scope: nextValue }))}
              options={[
                ['project', '项目'],
                ['session', '会话'],
              ]}
              value={draft.scope}
            />
          </label>
          <label>
            <span>类型</span>
            <SelectMenu
              ariaLabel="记忆类型"
              onChange={(nextValue) => setDraft((current) => ({ ...current, kind: nextValue }))}
              options={[
                ['fact', '事实'],
                ['preference', '偏好'],
                ['decision', '决策'],
                ['task', '任务'],
                ['summary', '摘要'],
                ['warning', '提醒'],
              ]}
              value={draft.kind}
            />
          </label>
          <label>
            <span>状态</span>
            <SelectMenu
              ariaLabel="记忆状态"
              onChange={(nextValue) => setDraft((current) => ({ ...current, status: nextValue }))}
              options={[
                ['active', '启用'],
                ['disabled', '停用'],
              ]}
              value={draft.status}
            />
          </label>
          <label>
            <span>置信度</span>
            <SelectMenu
              ariaLabel="记忆置信度"
              onChange={(nextValue) => setDraft((current) => ({ ...current, confidence: nextValue }))}
              options={[
                ['low', '低'],
                ['medium', '中'],
                ['high', '高'],
              ]}
              value={draft.confidence}
            />
          </label>
        </div>
        <Field className="memory-field" label="标题">
          <input
            data-testid="memory-title-input"
            onChange={(event) => setDraft((current) => ({ ...current, title: event.target.value }))}
            value={draft.title}
          />
        </Field>
        <Field className="memory-field" label="内容">
          <textarea
            data-testid="memory-content-input"
            onChange={(event) => setDraft((current) => ({ ...current, content: event.target.value }))}
            value={draft.content}
          />
        </Field>
        <div className="memory-actions">
          <Button
            data-testid="memory-save"
            disabled={saving || !draft.content.trim() || (draft.scope === 'project' && !workspaceRoot) || (draft.scope === 'session' && !currentSessionId)}
            icon={<Save size={14} />}
            onClick={saveMemory}
          >
            保存
          </Button>
          <Button disabled={!selected || saving || selected.status !== 'active'} onClick={disableMemory} variant="soft">
            停用
          </Button>
          <IconButton disabled={!selected || saving || selected.status === 'deleted'} label="删除记忆" onClick={deleteMemory} variant="ghost">
            <Trash2 size={14} />
          </IconButton>
        </div>
        {selected ? <span className="memory-updated">更新于 {compactTime(selected.updatedAt)}</span> : null}
      </div>

      <div className="memory-preview">
        <div className="memory-editor-head">
          <strong>预览</strong>
          <Button icon={<Eye size={14} />} onClick={previewMemory} variant="ghost">
            预览
          </Button>
        </div>
        {preview ? (
          <>
            <span>{preview.items.length} 条记录</span>
            <pre data-testid="memory-preview-context">{preview.context || '未选择记忆'}</pre>
          </>
        ) : null}
      </div>

      <ErrorMessage className="activity-error">{error}</ErrorMessage>
    </section>
  );
}
