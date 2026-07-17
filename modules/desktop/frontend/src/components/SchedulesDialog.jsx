import { useCallback, useEffect, useMemo, useState } from 'react';
import { Pause, Play, Plus, RefreshCw, Trash2, Zap } from 'lucide-react';
import {
  emptyScheduleDraft,
  formatScheduleTime,
  normalizeSchedule,
  normalizeScheduleRun,
  scheduleCreatePayload,
  scheduleSummary,
} from '../lib/schedules.js';
import { workspaceDisplayName } from '../lib/sessionTree.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button, IconButton } from './ui/button.jsx';
import { Dialog, useOptionalDialog } from './ui/dialog.jsx';
import { EmptyState, ErrorMessage } from './ui/feedback.jsx';
import { Field } from './ui/field.jsx';
import { SelectMenu } from './ui/select.jsx';

function workspaceRootOf(item) {
  return String(item?.root_path || item?.root || item?.workspaceRoot || '').trim();
}

/**
 * 定时任务管理弹窗：Gateway /api/v1/schedules。
 */
export function SchedulesDialog({
  open = false,
  onClose,
  apiJson,
  workspaceRoot = '',
  workspaces = [],
  providerProfileId = '',
  onOpenSession,
}) {
  const dialog = useOptionalDialog();
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState({ ...emptyScheduleDraft, workspaceRoot });
  const [creating, setCreating] = useState(false);
  const [selectedId, setSelectedId] = useState('');
  const [runs, setRuns] = useState([]);
  const [busyId, setBusyId] = useState('');

  const workspaceOptions = useMemo(() => {
    const map = new Map();
    for (const item of workspaces || []) {
      const root = workspaceRootOf(item);
      if (!root || map.has(root)) continue;
      map.set(root, {
        value: root,
        label: item?.name || workspaceDisplayName(root),
      });
    }
    const current = String(workspaceRoot || '').trim();
    if (current && !map.has(current)) {
      map.set(current, {
        value: current,
        label: workspaceDisplayName(current),
      });
    }
    return Array.from(map.values()).map((option) => ({
      value: option.value,
      label: `${option.label} · ${option.value}`,
    }));
  }, [workspaces, workspaceRoot]);

  const load = useCallback(async () => {
    if (!apiJson) return;
    setLoading(true);
    setError('');
    try {
      const data = await apiJson('/api/v1/schedules');
      const list = Array.isArray(data) ? data.map(normalizeSchedule) : [];
      setItems(list);
    } catch (err) {
      setError(err?.message || '加载定时任务失败');
    } finally {
      setLoading(false);
    }
  }, [apiJson]);

  const loadRuns = useCallback(async (id) => {
    if (!apiJson || !id) {
      setRuns([]);
      return;
    }
    try {
      const data = await apiJson(`/api/v1/schedules/${encodeURIComponent(id)}/runs?limit=20`);
      setRuns(Array.isArray(data) ? data.map(normalizeScheduleRun) : []);
    } catch {
      setRuns([]);
    }
  }, [apiJson]);

  useEffect(() => {
    if (!open) return;
    const preferred = String(workspaceRoot || '').trim();
    const fallback = workspaceOptions[0]?.value || preferred || '';
    const initialRoot = preferred && workspaceOptions.some((o) => o.value === preferred)
      ? preferred
      : fallback;
    setDraft({ ...emptyScheduleDraft, workspaceRoot: initialRoot });
    setSelectedId('');
    setRuns([]);
    setError('');
    load();
  }, [open, load, workspaceRoot, workspaceOptions]);

  useEffect(() => {
    if (!open) return;
    loadRuns(selectedId);
  }, [selectedId, loadRuns, open]);

  async function handleCreate(e) {
    e?.preventDefault?.();
    if (!String(draft.workspaceRoot || '').trim()) {
      setError('请先选择工作区');
      return;
    }
    setCreating(true);
    setError('');
    try {
      const payload = scheduleCreatePayload(draft, {
        workspaceRoot: draft.workspaceRoot || workspaceRoot,
        providerProfileId,
      });
      const data = await apiJson('/api/v1/schedules', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      const item = normalizeSchedule(data);
      setItems((prev) => [item, ...prev.filter((x) => x.id !== item.id)]);
      const keepRoot = draft.workspaceRoot || workspaceRoot || workspaceOptions[0]?.value || '';
      setDraft({ ...emptyScheduleDraft, workspaceRoot: keepRoot });
      setSelectedId(item.id);
    } catch (err) {
      setError(err?.message || '创建失败');
    } finally {
      setCreating(false);
    }
  }

  async function toggleEnabled(item) {
    setBusyId(item.id);
    setError('');
    try {
      const path = item.enabled
        ? `/api/v1/schedules/${encodeURIComponent(item.id)}/disable`
        : `/api/v1/schedules/${encodeURIComponent(item.id)}/enable`;
      const data = await apiJson(path, { method: 'POST', body: '{}' });
      const next = normalizeSchedule(data);
      setItems((prev) => prev.map((x) => (x.id === next.id ? next : x)));
    } catch (err) {
      setError(err?.message || '更新失败');
    } finally {
      setBusyId('');
    }
  }

  async function handleTrigger(item) {
    setBusyId(item.id);
    setError('');
    try {
      const data = await apiJson(`/api/v1/schedules/${encodeURIComponent(item.id)}/trigger`, {
        method: 'POST',
        body: '{}',
      });
      if (data?.session_id && onOpenSession) {
        onOpenSession(data.session_id);
        onClose?.();
      }
      await load();
      if (selectedId === item.id) await loadRuns(item.id);
    } catch (err) {
      setError(err?.message || '触发失败');
    } finally {
      setBusyId('');
    }
  }

  async function handleDelete(item) {
    const ok = dialog
      ? await dialog.confirm({
        title: '删除定时任务',
        message: `确定删除「${item.name}」？此操作不可撤销。`,
        confirmLabel: '删除',
        tone: 'danger',
        testId: 'schedule-delete-confirm',
      })
      : window.confirm(`确定删除「${item.name}」？`);
    if (!ok) return;

    setBusyId(item.id);
    setError('');
    try {
      await apiJson(`/api/v1/schedules/${encodeURIComponent(item.id)}`, { method: 'DELETE' });
      setItems((prev) => prev.filter((x) => x.id !== item.id));
      if (selectedId === item.id) {
        setSelectedId('');
        setRuns([]);
      }
    } catch (err) {
      setError(err?.message || '删除失败');
    } finally {
      setBusyId('');
    }
  }

  function openSessionAndClose(sessionId) {
    if (!sessionId) return;
    onOpenSession?.(sessionId);
    onClose?.();
  }

  const selected = items.find((item) => item.id === selectedId) || null;

  const createForm = (
    <form className="schedule-create-form" onSubmit={handleCreate}>
      <div className="schedule-form-row">
        <Field label="名称">
          <input
            data-testid="schedule-name"
            onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            placeholder="morning-digest"
            required
            value={draft.name}
          />
        </Field>
        <Field label="类型">
          <SelectMenu
            onChange={(value) => setDraft((d) => ({ ...d, scheduleKind: value }))}
            options={[
              { value: 'interval', label: '间隔' },
              { value: 'cron', label: 'Cron' },
              { value: 'one_shot', label: '一次性' },
            ]}
            value={draft.scheduleKind}
          />
        </Field>
      </div>

      {draft.scheduleKind === 'interval' ? (
        <Field label="间隔（秒，≥60）">
          <input
            min={60}
            onChange={(e) => setDraft((d) => ({ ...d, intervalSec: Number(e.target.value) }))}
            type="number"
            value={draft.intervalSec}
          />
        </Field>
      ) : null}
      {draft.scheduleKind === 'cron' ? (
        <Field label="Cron（分 时 日 月 周）">
          <input
            onChange={(e) => setDraft((d) => ({ ...d, cronExpr: e.target.value }))}
            placeholder="0 9 * * 1-5"
            value={draft.cronExpr}
          />
        </Field>
      ) : null}
      {draft.scheduleKind === 'one_shot' ? (
        <Field label="执行时间（ISO）">
          <input
            onChange={(e) => setDraft((d) => ({ ...d, runAt: e.target.value }))}
            placeholder="2026-07-17T09:00:00"
            value={draft.runAt}
          />
        </Field>
      ) : null}

      <Field label="提示词">
        <textarea
          data-testid="schedule-prompt"
          onChange={(e) => setDraft((d) => ({ ...d, prompt: e.target.value }))}
          placeholder="只读汇总工作区变更…"
          required
          rows={3}
          value={draft.prompt}
        />
      </Field>
      <Field label="工作区">
        {workspaceOptions.length > 0 ? (
          <SelectMenu
            ariaLabel="选择工作区"
            onChange={(value) => setDraft((d) => ({ ...d, workspaceRoot: value }))}
            options={workspaceOptions}
            testId="schedule-workspace"
            value={draft.workspaceRoot || workspaceOptions[0]?.value || ''}
          />
        ) : (
          <p className="schedules-muted" data-testid="schedule-workspace-empty">
            暂无可用工作区，请先在左侧「选择工作区」。
          </p>
        )}
      </Field>
      <div className="schedule-form-actions">
        <Button
          data-testid="schedule-create"
          disabled={creating || workspaceOptions.length === 0}
          icon={<Plus size={14} />}
          type="submit"
          variant="default"
        >
          {creating ? '创建中…' : '创建任务'}
        </Button>
      </div>
    </form>
  );

  const detailPanel = selected ? (
    <div className="schedule-detail" data-testid="schedule-detail">
      <div className="schedule-detail-header">
        <div className="schedule-detail-title">
          <strong>{selected.name}</strong>
          <StatusBadge status={selected.enabled ? 'running' : 'paused'}>
            {selected.enabled ? '启用' : '暂停'}
          </StatusBadge>
        </div>
        <div className="schedule-detail-actions">
          <IconButton
            disabled={busyId === selected.id}
            label={selected.enabled ? '暂停' : '启用'}
            onClick={() => toggleEnabled(selected)}
            variant="ghost"
          >
            {selected.enabled ? <Pause size={14} /> : <Play size={14} />}
          </IconButton>
          <IconButton
            disabled={busyId === selected.id}
            label="立即执行"
            onClick={() => handleTrigger(selected)}
            variant="ghost"
          >
            <Zap size={14} />
          </IconButton>
          <IconButton
            disabled={busyId === selected.id}
            label="删除"
            onClick={() => handleDelete(selected)}
            variant="ghost"
          >
            <Trash2 size={14} />
          </IconButton>
        </div>
      </div>

      <dl className="schedule-detail-fields">
        <div>
          <dt>调度</dt>
          <dd>{scheduleSummary(selected)}</dd>
        </div>
        <div>
          <dt>下次执行</dt>
          <dd>{formatScheduleTime(selected.nextRunAt)}</dd>
        </div>
        <div>
          <dt>上次状态</dt>
          <dd>{selected.lastStatus || '—'}</dd>
        </div>
        <div>
          <dt>工作区</dt>
          <dd className="schedule-detail-path" title={selected.workspaceRoot}>
            {selected.workspaceRoot || '—'}
          </dd>
        </div>
        <div className="schedule-detail-prompt">
          <dt>提示词</dt>
          <dd>{selected.prompt || '—'}</dd>
        </div>
      </dl>

      <div className="schedule-runs" data-testid="schedule-runs">
        <div className="schedules-section-title">执行历史</div>
        {runs.length === 0 ? <p className="schedules-muted">暂无记录</p> : null}
        {runs.map((run) => (
          <div className="schedule-run-row" key={run.id}>
            <StatusBadge status={run.status}>{run.status || 'unknown'}</StatusBadge>
            <span className="schedules-muted">{formatScheduleTime(run.createdAt)}</span>
            {run.sessionId && onOpenSession ? (
              <button
                className="schedule-run-link"
                onClick={() => openSessionAndClose(run.sessionId)}
                type="button"
              >
                打开会话
              </button>
            ) : null}
            {run.error ? <span className="error-text">{run.error}</span> : null}
          </div>
        ))}
      </div>
    </div>
  ) : null;

  return (
    <Dialog
      className="schedules-dialog"
      footer={(
        <>
          <Button
            data-testid="schedule-refresh"
            icon={<RefreshCw size={14} />}
            onClick={load}
            variant="ghost"
          >
            刷新
          </Button>
          <Button onClick={onClose} variant="soft">关闭</Button>
        </>
      )}
      onClose={onClose}
      open={open}
      size="xl"
      testId="schedules-dialog"
      title="定时任务"
    >
      <div className="schedules-dialog-body" data-testid="schedules-panel">
        {error ? <ErrorMessage className="schedules-dialog-error">{error}</ErrorMessage> : null}

        <div className="schedules-dialog-grid">
          <section
            aria-label={selected ? '任务详情' : '创建定时任务'}
            className="schedules-dialog-main"
          >
            <div className="schedules-section-title schedules-section-title-row">
              <span>{selected ? '任务详情' : '新建任务'}</span>
              {selected ? (
                <Button
                  data-testid="schedule-new"
                  icon={<Plus size={13} />}
                  onClick={() => {
                    setSelectedId('');
                    setRuns([]);
                  }}
                  variant="ghost"
                >
                  新建
                </Button>
              ) : null}
            </div>
            {selected ? detailPanel : createForm}
          </section>

          <section className="schedules-dialog-list" aria-label="任务列表">
            <div className="schedules-section-title">
              <span>任务列表</span>
              {items.length > 0 ? <em>{items.length}</em> : null}
            </div>

            <div className="schedule-list">
              {loading && items.length === 0 ? (
                <p className="schedules-muted">加载中…</p>
              ) : null}
              {!loading && items.length === 0 ? (
                <EmptyState title="暂无定时任务">创建一个每日摘要或间隔检查任务。</EmptyState>
              ) : null}
              {items.map((item) => (
                <div
                  className={`schedule-row ${selectedId === item.id ? 'is-selected' : ''}`}
                  data-testid={`schedule-row-${item.id}`}
                  key={item.id}
                  onClick={() => setSelectedId(item.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      setSelectedId(item.id);
                    }
                  }}
                  role="button"
                  tabIndex={0}
                >
                  <div className="schedule-row-main">
                    <strong>{item.name}</strong>
                    <StatusBadge status={item.enabled ? 'running' : 'paused'}>
                      {item.enabled ? '启用' : '暂停'}
                    </StatusBadge>
                  </div>
                  <div className="schedule-row-meta">
                    <span>{scheduleSummary(item)}</span>
                    <span>下次 {formatScheduleTime(item.nextRunAt)}</span>
                    {item.lastStatus ? <span>上次 {item.lastStatus}</span> : null}
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>
      </div>
    </Dialog>
  );
}
