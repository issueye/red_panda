import {
  ArrowLeft,
  Database,
  Eye,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  displayConfidence,
  displayMemoryKind,
  displayMemoryScope,
} from "../lib/displayLabels.js";
import {
  emptyMemoryDraft,
  memoryCreatePayload,
  memoryDraftFrom,
  memoryListQuery,
  memoryPreviewPayload,
  memoryUpdatePayload,
  normalizeMemoryRecord,
} from "../lib/memory.js";
import { classNames } from "../lib/format.js";
import { StatusBadge } from "./ui/badge.jsx";
import { Button, IconButton } from "./ui/button.jsx";
import { useOptionalDialog } from "./ui/dialog.jsx";
import { EmptyState, ErrorMessage, InlineEmpty } from "./ui/feedback.jsx";
import { Field } from "./ui/field.jsx";
import { PanelHeader } from "./ui/panel.jsx";
import { SelectMenu } from "./ui/select.jsx";
import { useOptionalToast } from "./ui/toast.jsx";

function compactTime(value) {
  if (!value) return "未记录";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未记录";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function uniqueById(items) {
  const seen = new Set();
  return items.filter((item) => {
    if (!item.id || seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
}

function previewText(item) {
  const text = String(item?.content || item?.title || "")
    .trim()
    .replace(/\s+/g, " ");
  if (!text) return "无内容";
  return text.length > 96 ? `${text.slice(0, 96)}…` : text;
}

function emptyListMessage({
  loading,
  workspaceRoot,
  currentSessionId,
  scopeFilter,
}) {
  if (loading) return { title: "正在加载记忆", detail: "请稍候…" };
  if (!workspaceRoot && (scopeFilter === "all" || scopeFilter === "project")) {
    return { title: "未打开工作区", detail: "打开工作区后可管理项目记忆。" };
  }
  if (
    !currentSessionId &&
    (scopeFilter === "all" || scopeFilter === "session")
  ) {
    return { title: "未选择会话", detail: "选择会话后可查看会话记忆。" };
  }
  return {
    title: "暂无记忆记录",
    detail: "新建一条事实、偏好或决策，供后续运行注入。",
  };
}

export function MemoryPanel({ apiJson, currentSessionId, workspaceRoot }) {
  const dialog = useOptionalDialog();
  const toast = useOptionalToast();
  const [items, setItems] = useState([]);
  const [scopeFilter, setScopeFilter] = useState("all");
  const [statusFilter, setStatusFilter] = useState("active");
  const [selectedId, setSelectedId] = useState("");
  const [draft, setDraft] = useState(emptyMemoryDraft);
  const [mode, setMode] = useState("list"); // list | edit
  const [preview, setPreview] = useState(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const selected = useMemo(
    () => items.find((item) => item.id === selectedId) || null,
    [items, selectedId],
  );
  const emptyCopy = emptyListMessage({
    loading,
    workspaceRoot,
    currentSessionId,
    scopeFilter,
  });

  async function loadMemory() {
    setLoading(true);
    setError("");
    try {
      const scopes =
        scopeFilter === "all" ? ["project", "session"] : [scopeFilter];
      const requests = scopes
        .filter((scope) =>
          scope === "project" ? workspaceRoot : currentSessionId,
        )
        .map((scope) =>
          apiJson(
            memoryListQuery({
              scope,
              status: statusFilter,
              workspaceRoot,
              sessionId: currentSessionId,
            }),
          ).catch(() => []),
        );
      const results = await Promise.all(requests);
      const normalized = uniqueById(
        results.flat().map(normalizeMemoryRecord),
      ).sort(
        (left, right) =>
          new Date(right.updatedAt || 0) - new Date(left.updatedAt || 0),
      );
      setItems(normalized);
      if (selectedId && !normalized.some((item) => item.id === selectedId)) {
        // Keep editor draft if user is creating; only clear when the selected record vanished.
        if (mode === "edit" && selectedId) {
          setMode("list");
          clearSelection();
        }
      }
    } catch (err) {
      setError(err.message || String(err));
    } finally {
      setLoading(false);
    }
  }

  function clearSelection() {
    setSelectedId("");
    setDraft(emptyMemoryDraft);
  }

  function backToList() {
    setMode("list");
    clearSelection();
    setError("");
  }

  function selectMemory(item) {
    setSelectedId(item.id);
    setDraft(memoryDraftFrom(item));
    setMode("edit");
    setError("");
  }

  function startCreate() {
    clearSelection();
    setDraft(emptyMemoryDraft);
    setMode("edit");
    setError("");
  }

  async function saveMemory() {
    setSaving(true);
    setError("");
    try {
      const saved = selected
        ? await apiJson(`/api/v1/memory/${encodeURIComponent(selected.id)}`, {
            method: "PUT",
            body: JSON.stringify(memoryUpdatePayload(draft)),
          })
        : await apiJson("/api/v1/memory", {
            method: "POST",
            body: JSON.stringify(
              memoryCreatePayload(draft, {
                workspaceRoot,
                sessionId: currentSessionId,
              }),
            ),
          });
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) => [
        normalized,
        ...current.filter((item) => item.id !== normalized.id),
      ]);
      setSelectedId(normalized.id);
      setDraft(memoryDraftFrom(normalized));
      toast?.success(selected ? "记忆已更新" : "记忆已创建");
    } catch (err) {
      const message = err.message || String(err);
      setError(message);
      toast?.error(message, { title: "保存记忆失败" });
    } finally {
      setSaving(false);
    }
  }

  async function disableMemory() {
    if (!selected) return;
    setDraft((current) => ({ ...current, status: "disabled" }));
    setSaving(true);
    setError("");
    try {
      const saved = await apiJson(
        `/api/v1/memory/${encodeURIComponent(selected.id)}`,
        {
          method: "PUT",
          body: JSON.stringify({ status: "disabled" }),
        },
      );
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) =>
        current
          .map((item) => (item.id === normalized.id ? normalized : item))
          .filter(
            (item) => statusFilter === "all" || item.status === statusFilter,
          ),
      );
      toast?.success("记忆已停用");
      setMode("list");
      clearSelection();
    } catch (err) {
      const message = err.message || String(err);
      setError(message);
      toast?.error(message, { title: "停用失败" });
    } finally {
      setSaving(false);
    }
  }

  async function deleteMemory() {
    if (!selected) return;
    const title = selected.title || selected.id;
    const ok = dialog
      ? await dialog.confirm({
          title: "删除记忆",
          message: `确定删除「${title}」？`,
          description: "记录将标记为已删除，默认列表不再显示。",
          confirmLabel: "删除",
          tone: "danger",
          testId: "confirm-delete-memory",
        })
      : typeof window !== "undefined"
        ? window.confirm(`确定删除「${title}」？`)
        : true;
    if (!ok) return;

    setSaving(true);
    setError("");
    try {
      const saved = await apiJson(
        `/api/v1/memory/${encodeURIComponent(selected.id)}`,
        { method: "DELETE" },
      );
      const normalized = normalizeMemoryRecord(saved);
      setItems((current) =>
        current
          .map((item) => (item.id === normalized.id ? normalized : item))
          .filter(
            (item) => statusFilter === "all" || item.status === statusFilter,
          ),
      );
      toast?.success(`已删除记忆「${title}」`);
      setMode("list");
      clearSelection();
    } catch (err) {
      const message = err.message || String(err);
      setError(message);
      toast?.error(message, { title: "删除失败" });
    } finally {
      setSaving(false);
    }
  }

  async function previewMemory() {
    setLoading(true);
    setError("");
    try {
      const data = await apiJson("/api/v1/memory/preview-run", {
        method: "POST",
        body: JSON.stringify(
          memoryPreviewPayload({
            sessionId: currentSessionId,
            workspaceRoot,
            input: "",
          }),
        ),
      });
      setPreview({
        items: Array.isArray(data.items)
          ? data.items.map(normalizeMemoryRecord)
          : [],
        context: data.context || "",
      });
      setPreviewOpen(true);
    } catch (err) {
      setPreview(null);
      const message = err.message || String(err);
      setError(message);
      toast?.error(message, { title: "预览失败" });
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadMemory();
  }, [currentSessionId, workspaceRoot, scopeFilter, statusFilter]);

  if (mode === "edit") {
    return (
      <section
        className="memory-panel-content is-edit"
        data-testid="memory-panel"
        data-mode="edit"
      >
        <div className="memory-edit-header">
          <IconButton
            data-testid="memory-back"
            label="返回列表"
            onClick={backToList}
          >
            <ArrowLeft size={15} />
          </IconButton>
          <div className="memory-edit-heading">
            <strong>{selected ? "编辑记忆" : "新建记忆"}</strong>
            <span>
              {selected
                ? compactTime(selected.updatedAt)
                : "填写后保存，可在后续运行中注入"}
            </span>
          </div>
        </div>

        <div className="memory-editor" data-testid="memory-editor">
          <div className="memory-editor-grid">
            <label>
              <span>范围</span>
              <SelectMenu
                ariaLabel="记忆范围"
                disabled={Boolean(selected)}
                onChange={(nextValue) =>
                  setDraft((current) => ({ ...current, scope: nextValue }))
                }
                options={[
                  ["project", "项目"],
                  ["session", "会话"],
                ]}
                value={draft.scope}
              />
            </label>
            <label>
              <span>类型</span>
              <SelectMenu
                ariaLabel="记忆类型"
                onChange={(nextValue) =>
                  setDraft((current) => ({ ...current, kind: nextValue }))
                }
                options={[
                  ["fact", "事实"],
                  ["preference", "偏好"],
                  ["decision", "决策"],
                  ["task", "任务"],
                  ["summary", "摘要"],
                  ["warning", "提醒"],
                ]}
                value={draft.kind}
              />
            </label>
            <label>
              <span>状态</span>
              <SelectMenu
                ariaLabel="记忆状态"
                onChange={(nextValue) =>
                  setDraft((current) => ({ ...current, status: nextValue }))
                }
                options={[
                  ["active", "启用"],
                  ["disabled", "停用"],
                ]}
                value={draft.status}
              />
            </label>
            <label>
              <span>置信度</span>
              <SelectMenu
                ariaLabel="记忆置信度"
                onChange={(nextValue) =>
                  setDraft((current) => ({ ...current, confidence: nextValue }))
                }
                options={[
                  ["low", "低"],
                  ["medium", "中"],
                  ["high", "高"],
                ]}
                value={draft.confidence}
              />
            </label>
          </div>
          <Field className="memory-field" label="标题">
            <input
              data-testid="memory-title-input"
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  title: event.target.value,
                }))
              }
              value={draft.title}
            />
          </Field>
          <Field className="memory-field" label="内容">
            <textarea
              data-testid="memory-content-input"
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  content: event.target.value,
                }))
              }
              value={draft.content}
            />
          </Field>
          <div className="memory-actions">
            <Button
              data-testid="memory-save"
              disabled={
                saving ||
                !draft.content.trim() ||
                (draft.scope === "project" && !workspaceRoot) ||
                (draft.scope === "session" && !currentSessionId)
              }
              icon={<Save size={14} />}
              loading={saving}
              onClick={saveMemory}
            >
              保存
            </Button>
            <Button
              disabled={!selected || saving || selected.status !== "active"}
              onClick={disableMemory}
              variant="soft"
            >
              停用
            </Button>
            <IconButton
              disabled={!selected || saving || selected.status === "deleted"}
              label="删除记忆"
              onClick={deleteMemory}
              variant="ghost"
            >
              <Trash2 size={14} />
            </IconButton>
          </div>
          {selected ? (
            <span className="memory-updated">
              更新于 {compactTime(selected.updatedAt)}
            </span>
          ) : null}
        </div>

        <ErrorMessage className="activity-error">{error}</ErrorMessage>
      </section>
    );
  }

  return (
    <section
      className="memory-panel-content is-list"
      data-testid="memory-panel"
      data-mode="list"
    >
      <PanelHeader
        action={
          <div className="memory-header-actions">
            <IconButton label="刷新记忆" onClick={loadMemory}>
              <RefreshCw size={14} />
            </IconButton>
            <Button
              data-testid="memory-create"
              icon={<Plus size={14} />}
              onClick={startCreate}
              variant="soft"
            >
              新建
            </Button>
          </div>
        }
        title="记忆"
      />

      <div className="memory-toolbar">
        <SelectMenu
          aria-label="记忆范围筛选"
          ariaLabel="记忆范围筛选"
          onChange={setScopeFilter}
          options={[
            ["all", "全部"],
            ["project", "项目"],
            ["session", "会话"],
          ]}
          value={scopeFilter}
        />
        <SelectMenu
          aria-label="记忆状态筛选"
          ariaLabel="记忆状态筛选"
          onChange={setStatusFilter}
          options={[
            ["active", "启用"],
            ["disabled", "停用"],
            ["all", "全部"],
          ]}
          value={statusFilter}
        />
      </div>

      <div className="memory-list" data-testid="memory-list">
        {items.length === 0 ? (
          <EmptyState title={emptyCopy.title}>{emptyCopy.detail}</EmptyState>
        ) : (
          items.map((item) => (
            <button
              className={classNames(
                "memory-item",
                item.id === selectedId && "active",
              )}
              data-testid="memory-item"
              key={item.id}
              onClick={() => selectMemory(item)}
              type="button"
            >
              <span
                className={classNames(
                  "memory-kind-dot",
                  `is-${item.kind || "fact"}`,
                )}
                aria-hidden="true"
              >
                <Database size={13} />
              </span>
              <span className="memory-item-body">
                <strong>{item.title || "未命名记忆"}</strong>
                <em className="memory-item-preview">{previewText(item)}</em>
                <em>
                  {displayMemoryScope(item.scope)}
                  {" · "}
                  {displayMemoryKind(item.kind)}
                  {" · "}
                  {compactTime(item.updatedAt)}
                </em>
              </span>
              <StatusBadge
                className={`memory-status memory-status-${item.status}`}
                status={item.status}
              />
            </button>
          ))
        )}
      </div>

      <div className="memory-preview">
        <div className="memory-editor-head">
          <strong>注入预览</strong>
          <Button
            icon={<Eye size={14} />}
            onClick={previewMemory}
            variant="ghost"
          >
            预览
          </Button>
        </div>
        {previewOpen && preview ? (
          <>
            <span>{preview.items.length} 条将注入</span>
            <pre data-testid="memory-preview-context">
              {preview.context || "未选择记忆"}
            </pre>
          </>
        ) : (
          <InlineEmpty className="activity-empty">
            预览当前会注入 Provider 的记忆上下文。
          </InlineEmpty>
        )}
      </div>

      <ErrorMessage className="activity-error">{error}</ErrorMessage>
    </section>
  );
}
