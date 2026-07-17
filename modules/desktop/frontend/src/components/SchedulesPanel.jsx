import { useCallback, useEffect, useState } from 'react';
import { Pause, Play, Plus, RefreshCw, Trash2, Zap } from 'lucide-react';
import {
  emptyScheduleDraft,
  formatScheduleTime,
  normalizeSchedule,
  normalizeScheduleRun,
  scheduleCreatePayload,
  scheduleSummary,
} from '../lib/schedules.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button, IconButton } from './ui/button.jsx';
import { EmptyState, ErrorMessage } from './ui/feedback.jsx';
import { Field } from './ui/field.jsx';
import { PanelHeader } from './ui/panel.jsx';
import { SelectMenu } from './ui/select.jsx';

/**
 * 定时任务管理：Gateway /api/v1/schedules。
 */
export function SchedulesPanel({
  apiJson,
  workspaceRoot = '',
  providerProfileId = '',
  onOpenSession,
}) {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState({ ...emptyScheduleDraft, workspaceRoot });
  const [creating, setCreating] = useState(false);
  const [selectedId, setSelectedId] = useState('');
  const [runs, setRuns] = useState([]);
  const [busyId, setBusyId] = useState('');

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
    load();
  }, [load]);

  useEffect(() => {
    setDraft((current) => ({ ...current, workspaceRoot: workspaceRoot || current.workspaceRoot }));
  }, [workspaceRoot]);

  useEffect(() => {
    loadRuns(selectedId);
  }, [selectedId, loadRuns]);

  async function handleCreate(e) {
    e?.preventDefault?.();
    setCreating(true);
    setError('');
    try {
      const payload = scheduleCreatePayload(draft, { workspaceRoot, providerProfileId });
      const data = await apiJson('/api/v1/schedules', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      const item = normalizeSchedule(data);
      setItems((prev) => [item, ...prev.filter((x) => x.id !== item.id)]);
      setDraft({ ...emptyScheduleDraft, workspaceRoot });
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

  return (
    <div className="panel schedules-panel" data-testid="schedules-panel">
      <PanelHeader
        title="定时任务"
        subtitle="Gateway 调度；默认拒绝高风险工具"
        action={(
          <IconButton aria-label="刷新" onClick={load} title="刷新" variant="ghost">
            <RefreshCw size={14} />
          </IconButton>
        )}
      />
      {error ? <ErrorMessage className="activity-error">{error}</ErrorMessage> : null}

      <form className="schedule-create-form" onSubmit={handleCreate}>
        <Field label="名称">
          <input
            data-testid="schedule-name"
            value={draft.name}
            onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            placeholder="morning-digest"
            required
          />
        </Field>
        <Field label="类型">
          <SelectMenu
            value={draft.scheduleKind}
            onChange={(value) => setDraft((d) => ({ ...d, scheduleKind: value }))}
            options={[
              { value: 'interval', label: '间隔' },
              { value: 'cron', label: 'Cron' },
              { value: 'one_shot', label: '一次性' },
            ]}
          />
        </Field>
        {draft.scheduleKind === 'interval' ? (
          <Field label="间隔（秒，≥60）">
            <input
              type="number"
              min={60}
              value={draft.intervalSec}
              onChange={(e) => setDraft((d) => ({ ...d, intervalSec: Number(e.target.value) }))}
            />
          </Field>
        ) : null}
        {draft.scheduleKind === 'cron' ? (
          <Field label="Cron（分 时 日 月 周）">
            <input
              value={draft.cronExpr}
              onChange={(e) => setDraft((d) => ({ ...d, cronExpr: e.target.value }))}
              placeholder="0 9 * * 1-5"
            />
          </Field>
        ) : null}
        {draft.scheduleKind === 'one_shot' ? (
          <Field label="执行时间（ISO）">
            <input
              value={draft.runAt}
              onChange={(e) => setDraft((d) => ({ ...d, runAt: e.target.value }))}
              placeholder="2026-07-17T09:00:00"
            />
          </Field>
        ) : null}
        <Field label="提示词">
          <textarea
            data-testid="schedule-prompt"
            rows={3}
            value={draft.prompt}
            onChange={(e) => setDraft((d) => ({ ...d, prompt: e.target.value }))}
            placeholder="只读汇总工作区变更…"
            required
          />
        </Field>
        <Field label="工作区">
          <input
            value={draft.workspaceRoot}
            onChange={(e) => setDraft((d) => ({ ...d, workspaceRoot: e.target.value }))}
            placeholder={workspaceRoot || 'E:/path/to/workspace'}
            required
          />
        </Field>
        <Button
          data-testid="schedule-create"
          disabled={creating}
          icon={<Plus size={14} />}
          type="submit"
          variant="default"
        >
          {creating ? '创建中…' : '创建'}
        </Button>
      </form>

      <div className="schedule-list">
        {loading && items.length === 0 ? <p className="muted">加载中…</p> : null}
        {!loading && items.length === 0 ? (
          <EmptyState title="暂无定时任务">创建一个每日摘要或间隔检查任务。</EmptyState>
        ) : null}
        {items.map((item) => (
          <div
            key={item.id}
            className={`schedule-row ${selectedId === item.id ? 'is-selected' : ''}`}
            data-testid={`schedule-row-${item.id}`}
            onClick={() => setSelectedId(item.id)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') setSelectedId(item.id);
            }}
            role="button"
            tabIndex={0}
          >
            <div className="schedule-row-main">
              <strong>{item.name}</strong>
              <span className="muted">{scheduleSummary(item)}</span>
              <StatusBadge status={item.enabled ? 'running' : 'paused'}>
                {item.enabled ? '启用' : '暂停'}
              </StatusBadge>
              {item.lastStatus ? <span className="muted">上次 {item.lastStatus}</span> : null}
            </div>
            <div className="schedule-row-meta muted">
              下次 {formatScheduleTime(item.nextRunAt)}
            </div>
            <div className="schedule-row-actions" onClick={(e) => e.stopPropagation()}>
              <IconButton
                aria-label={item.enabled ? '暂停' : '启用'}
                disabled={busyId === item.id}
                onClick={() => toggleEnabled(item)}
                title={item.enabled ? '暂停' : '启用'}
                variant="ghost"
              >
                {item.enabled ? <Pause size={14} /> : <Play size={14} />}
              </IconButton>
              <IconButton
                aria-label="立即执行"
                disabled={busyId === item.id}
                onClick={() => handleTrigger(item)}
                title="立即执行"
                variant="ghost"
              >
                <Zap size={14} />
              </IconButton>
              <IconButton
                aria-label="删除"
                disabled={busyId === item.id}
                onClick={() => handleDelete(item)}
                title="删除"
                variant="ghost"
              >
                <Trash2 size={14} />
              </IconButton>
            </div>
          </div>
        ))}
      </div>

      {selectedId ? (
        <div className="schedule-runs" data-testid="schedule-runs">
          <h4>执行历史</h4>
          {runs.length === 0 ? <p className="muted">暂无记录</p> : null}
          {runs.map((run) => (
            <div key={run.id} className="schedule-run-row">
              <span>{run.status}</span>
              <span className="muted">{formatScheduleTime(run.createdAt)}</span>
              {run.sessionId && onOpenSession ? (
                <button
                  className="linkish"
                  type="button"
                  onClick={() => onOpenSession(run.sessionId)}
                >
                  打开会话
                </button>
              ) : null}
              {run.error ? <span className="error-text">{run.error}</span> : null}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
