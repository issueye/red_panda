import {
  CheckCircle2,
  ChevronDown,
  Copy,
  Loader2,
  ShieldAlert,
  Wrench,
  XCircle,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { displayRisk, displayStatus } from '../lib/displayLabels.js';
import { classNames } from '../lib/format.js';
import { IconButton } from './ui/button.jsx';
import { StatusBadge } from './ui/badge.jsx';

/**
 * 根据工具状态返回对应图标。
 * @param {string} status 工具状态
 * @returns {JSX.Element} 状态图标
 */
function statusIcon(status) {
  if (status === 'completed') return <CheckCircle2 size={14} />;
  if (status === 'failed' || status === 'denied') return <XCircle size={14} />;
  if (status === 'waiting_permission' || status === 'pending') return <ShieldAlert size={14} />;
  return <Loader2 className="tool-spin" size={14} />;
}

/**
 * 将任意值格式化为适合展示的文本。
 * @param {unknown} value 原始值
 * @returns {string} 展示文本
 */
function formatValue(value) {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

/**
 * 格式化耗时展示。
 * @param {number | undefined} durationMs 毫秒耗时
 * @returns {string} 耗时文案
 */
function formatDuration(durationMs) {
  const value = Number(durationMs);
  if (!Number.isFinite(value) || value < 0) return '';
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)} s`;
}

/**
 * 从工具参数中提取一句话摘要，便于快速扫读。
 * @param {string} name 工具名
 * @param {Record<string, unknown> | string | undefined} args 参数
 * @returns {string} 摘要文本
 */
function buildToolSummary(name, args) {
  if (!args || typeof args !== 'object' || Array.isArray(args)) {
    if (typeof args === 'string' && args.trim()) return args.trim();
    return '';
  }

  const pathLike = args.path || args.file || args.filepath || args.target || args.uri || args.url;
  if (pathLike) return String(pathLike);

  if (args.command) return String(args.command);
  if (args.query) return String(args.query);
  if (args.pattern) return String(args.pattern);
  if (args.skill || args.skill_name) return String(args.skill || args.skill_name);
  if (args.content && typeof args.content === 'string') {
    const text = args.content.trim().replace(/\s+/g, ' ');
    return text.length > 96 ? `${text.slice(0, 96)}…` : text;
  }

  const keys = Object.keys(args);
  if (keys.length === 0) return '';
  if (keys.length === 1) {
    const only = formatValue(args[keys[0]]).replace(/\s+/g, ' ').trim();
    return only.length > 96 ? `${only.slice(0, 96)}…` : only;
  }
  return `${keys.length} 个参数`;
}

/**
 * 统计输出行数，用于折叠摘要。
 * @param {string} text 输出文本
 * @returns {number} 行数
 */
function countLines(text) {
  if (!text) return 0;
  return text.split(/\r?\n/).length;
}

/**
 * 判断文本是否较短，适合默认展开。
 * @param {string} text 文本
 * @returns {boolean} 是否短文本
 */
function isShortText(text) {
  if (!text) return true;
  return text.length <= 480 && countLines(text) <= 8;
}

/**
 * 截取预览文本。
 * @param {string} text 原文
 * @param {number} max 最大长度
 * @returns {string} 预览
 */
function previewText(text, max = 120) {
  const normalized = String(text || '').replace(/\s+/g, ' ').trim();
  if (!normalized) return '';
  return normalized.length > max ? `${normalized.slice(0, max)}…` : normalized;
}

/**
 * 复制文本到剪贴板。
 * @param {string} text 待复制内容
 * @returns {Promise<boolean>} 是否成功
 */
async function copyText(text) {
  if (!text) return false;
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fallback below
  }
  try {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.left = '-9999px';
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(area);
    return ok;
  } catch {
    return false;
  }
}

/**
 * 可折叠内容区，支持复制与稳定高度的输出滚动。
 * @param {{
 *  title: string,
 *  text: string,
 *  defaultOpen?: boolean,
 *  open?: boolean,
 *  onOpenChange?: (open: boolean) => void,
 *  className?: string,
 *  testId?: string,
 *  showCollapsedPreview?: boolean,
 * }} props 组件属性
 */
function ToolSection({
  title,
  text,
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange,
  className,
  testId,
  showCollapsedPreview = false,
}) {
  const [uncontrolledOpen, setUncontrolledOpen] = useState(defaultOpen);
  const open = controlledOpen ?? uncontrolledOpen;
  const collapsedPreview = !open && showCollapsedPreview ? previewText(text) : '';

  /**
   * 切换折叠状态。
   */
  function toggle() {
    const next = !open;
    if (controlledOpen === undefined) setUncontrolledOpen(next);
    onOpenChange?.(next);
  }

  /**
   * 复制当前区块内容。
   * @param {MouseEvent} event 点击事件
   */
  async function handleCopy(event) {
    event.preventDefault();
    event.stopPropagation();
    await copyText(text);
  }

  if (!text) return null;

  return (
    <div className={classNames('tool-section', className, open && 'is-open')}>
      <div className="tool-section-header">
        <button
          aria-expanded={open}
          className="tool-section-toggle"
          onClick={toggle}
          type="button"
        >
          <ChevronDown className={classNames('tool-chevron', open && 'is-open')} size={14} />
          <span>{title}</span>
          <small>{countLines(text)} 行</small>
        </button>
        <IconButton
          className="tool-copy"
          data-testid={testId ? `${testId}-copy` : undefined}
          label={`复制${title}`}
          onClick={handleCopy}
          type="button"
          variant="ghost"
        >
          <Copy size={13} />
        </IconButton>
      </div>
      {open ? (
        <pre className="tool-pre" data-testid={testId}>{text}</pre>
      ) : collapsedPreview ? (
        <p className="tool-collapsed-preview" data-testid={testId}>{collapsedPreview}</p>
      ) : null}
    </div>
  );
}

/**
 * 对话时间线中的工具调用卡片。
 * @param {{ item: {
 *  name?: string,
 *  displayName?: string,
 *  risk?: string,
 *  status?: string,
 *  arguments?: Record<string, unknown> | string,
 *  output?: string,
 *  error?: string,
 *  durationMs?: number,
 * }}} props 组件属性
 */
export function ToolCallCard({ item }) {
  const status = item.status || 'running';
  const isRunning = status === 'running' || status === 'pending' || status === 'waiting_permission';
  const argsText = useMemo(() => {
    if (!item.arguments) return '';
    if (typeof item.arguments === 'string') return item.arguments;
    const keys = Object.keys(item.arguments);
    if (keys.length === 0) return '';
    return formatValue(item.arguments);
  }, [item.arguments]);
  const outputText = item.output || '';
  const errorText = item.error || '';
  const summary = useMemo(
    () => buildToolSummary(item.name || '', item.arguments),
    [item.arguments, item.name],
  );
  const duration = formatDuration(item.durationMs);
  const shouldOpenOutput = isRunning || Boolean(errorText) || (Boolean(outputText) && isShortText(outputText));

  // 运行中/短输出/错误默认展开；长输出可折叠，折叠时仍显示预览行。
  const [argsOpen, setArgsOpen] = useState(false);
  const [outputOpen, setOutputOpen] = useState(shouldOpenOutput);

  useEffect(() => {
    if (isRunning) {
      setOutputOpen(true);
    }
  }, [isRunning]);

  useEffect(() => {
    if (errorText && !isRunning) {
      setOutputOpen(true);
    }
  }, [errorText, isRunning]);

  return (
    <article
      className={classNames('tool-card', `tool-${status}`)}
      data-testid="tool-card"
      data-timeline-type="tool"
    >
      <div className="tool-title">
        <div className="tool-icon" aria-hidden="true">
          <Wrench size={15} />
        </div>
        <div className="tool-heading">
          <div className="tool-heading-row">
            <strong>{item.displayName || item.name || '工具'}</strong>
            <StatusBadge
              className="tool-status"
              data-testid="tool-status"
              icon={statusIcon(status)}
              status={status}
            />
          </div>
          <div className="tool-meta">
            <span className="tool-name">{item.name || 'tool'}</span>
            <span className={classNames('tool-risk', `risk-${item.risk || 'low'}`)}>
              {displayRisk(item.risk || 'low')}风险
            </span>
            {duration ? <span className="tool-duration">{duration}</span> : null}
            {isRunning ? <span className="tool-live">{displayStatus(status)}</span> : null}
          </div>
          {summary ? (
            <p className="tool-summary" title={summary}>{summary}</p>
          ) : null}
        </div>
      </div>

      <ToolSection
        className="tool-args"
        defaultOpen={false}
        onOpenChange={setArgsOpen}
        open={argsOpen}
        testId="tool-args"
        text={argsText}
        title="参数"
      />

      {errorText ? (
        <div className="tool-error-box" data-testid="tool-error">
          <strong>错误</strong>
          <p className="tool-error">{errorText}</p>
        </div>
      ) : null}

      <ToolSection
        className="tool-output"
        defaultOpen={shouldOpenOutput}
        onOpenChange={setOutputOpen}
        open={outputOpen}
        showCollapsedPreview
        testId="tool-output"
        text={outputText}
        title={isRunning ? '输出（实时）' : '输出'}
      />
    </article>
  );
}
