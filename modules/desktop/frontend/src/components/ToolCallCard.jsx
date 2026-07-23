import {
  Check,
  ChevronRight,
  Copy,
  Loader2,
  ShieldAlert,
  X,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { classNames } from '../lib/format.js';
import { buildToolOutputSummary, displayToolOutput } from '../lib/toolResultDisplay.js';
import { shouldMarkToolStale } from '../lib/toolSelfManaged.js';
import { IconButton } from './ui/button.jsx';

/**
 * @param {string} status
 */
function statusIcon(status) {
  if (status === 'completed') return <Check size={13} strokeWidth={2.5} />;
  if (status === 'failed' || status === 'denied') return <X size={13} strokeWidth={2.5} />;
  if (status === 'waiting_permission' || status === 'pending') return <ShieldAlert size={13} />;
  return <Loader2 className="tool-spin" size={13} />;
}

/**
 * @param {unknown} value
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
 * @param {number | undefined} durationMs
 */
function formatDuration(durationMs) {
  const value = Number(durationMs);
  if (!Number.isFinite(value) || value < 0) return '';
  if (value < 1000) return `${Math.round(value)}ms`;
  return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}s`;
}

/**
 * @param {Record<string, unknown> | string | undefined} args
 */
function buildToolSummary(args) {
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
  return `${keys.length} 项参数`;
}

function countLines(text) {
  if (!text) return 0;
  return text.split(/\r?\n/).length;
}

async function copyText(text) {
  if (!text) return false;
  try {
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fallback
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
 * Compact collapsible block for args/output.
 */
function ToolDetail({ title, text, open, onToggle, testId }) {
  if (!text) return null;

  return (
    <div className={classNames('tool-detail', open && 'is-open')}>
      <div className="tool-detail-bar">
        <button
          aria-expanded={open}
          className="tool-detail-toggle"
          onClick={onToggle}
          type="button"
        >
          <ChevronRight className={classNames('tool-detail-chevron', open && 'is-open')} size={12} />
          <span>{title}</span>
          <em>{countLines(text)} 行</em>
        </button>
        <IconButton
          className="tool-copy"
          data-testid={testId ? `${testId}-copy` : undefined}
          label={`复制${title}`}
          onClick={async (event) => {
            event.preventDefault();
            event.stopPropagation();
            await copyText(text);
          }}
          type="button"
          variant="ghost"
        >
          <Copy size={12} />
        </IconButton>
      </div>
      {open ? <pre className="tool-pre" data-testid={testId}>{text}</pre> : null}
    </div>
  );
}

/**
 * Compact but readable tool-call card for the conversation timeline.
 * @param {{ item: Record<string, unknown>, callIndex?: number, callTotal?: number }} props
 */
export function ToolCallCard({ item, callIndex, callTotal }) {
  const status = item.status || 'running';
  const isRunning = status === 'running' || status === 'pending' || status === 'waiting_permission';
  const isFailed = status === 'failed' || status === 'denied';

  const argsText = useMemo(() => {
    if (!item.arguments) return '';
    if (typeof item.arguments === 'string') return item.arguments;
    const keys = Object.keys(item.arguments);
    if (keys.length === 0) return '';
    return formatValue(item.arguments);
  }, [item.arguments]);

  const outputText = useMemo(
    () => displayToolOutput(item.output || '', item.error || ''),
    [item.error, item.output],
  );
  const errorText = item.error || '';
  const argSummary = useMemo(() => buildToolSummary(item.arguments), [item.arguments]);
  const outputSummary = useMemo(() => buildToolOutputSummary(item.output || ''), [item.output]);
  const summary = argSummary || outputSummary;
  const title = item.displayName || item.name || '工具';
  const toolName = item.name && item.name !== title ? item.name : '';
  const hasBody = Boolean(argsText || errorText || outputText);
  const index = Number(callIndex ?? item.callIndex);
  const total = Number(callTotal ?? item.callTotal);
  const hasIndex = Number.isFinite(index) && index > 0;
  const hasTotal = Number.isFinite(total) && total > 0;
  const indexLabel = hasIndex
    ? (hasTotal ? `${index}/${total}` : `#${index}`)
    : '';
  const indexTitle = hasIndex
    ? (hasTotal ? `第 ${index} 次工具调用，共 ${total} 次` : `第 ${index} 次工具调用`)
    : '';

  const [cardOpen, setCardOpen] = useState(isRunning || isFailed);
  const [argsOpen, setArgsOpen] = useState(false);
  const [outputOpen, setOutputOpen] = useState(isRunning || isFailed);
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    if (isRunning) {
      setCardOpen(true);
      setOutputOpen(true);
      return;
    }
    if (isFailed || errorText) {
      setCardOpen(true);
      setOutputOpen(true);
      return;
    }
    setCardOpen(false);
    setOutputOpen(false);
  }, [isRunning, isFailed, errorText, status]);

  // Live elapsed timer so stuck "进行中" tools are visible instead of a silent hang.
  useEffect(() => {
    if (!isRunning) return undefined;
    setNowMs(Date.now());
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [isRunning, item.startedAt, item.id]);

  const elapsedMs = useMemo(() => {
    if (!isRunning) return Number(item.durationMs) || 0;
    const started = item.startedAt ? new Date(item.startedAt).getTime() : NaN;
    if (!Number.isFinite(started)) return 0;
    return Math.max(0, nowMs - started);
  }, [isRunning, item.durationMs, item.startedAt, nowMs]);

  const duration = formatDuration(isRunning ? elapsedMs : item.durationMs);
  const isStale = isRunning && shouldMarkToolStale(item.name, elapsedMs);

  return (
    <article
      className={classNames(
        'tool-card',
        `tool-${status}`,
        cardOpen ? 'is-expanded' : 'is-collapsed',
      )}
      data-testid="tool-card"
      data-timeline-type="tool"
    >
      <button
        aria-expanded={cardOpen}
        className="tool-card-header"
        data-testid="tool-card-toggle"
        disabled={!hasBody}
        onClick={() => {
          if (!hasBody) return;
          setCardOpen((current) => !current);
        }}
        type="button"
      >
        <span className={classNames('tool-status-dot', `is-${status}`)} aria-hidden="true">
          {statusIcon(status)}
        </span>

        <span className="tool-card-content">
          <span className="tool-card-title">
            {indexLabel ? (
              <span
                className="tool-call-index"
                data-testid="tool-call-index"
                title={indexTitle}
              >
                {indexLabel}
              </span>
            ) : null}
            <strong>{title}</strong>
            {toolName ? <code className="tool-card-name">{toolName}</code> : null}
          </span>
          {summary ? (
            <span className="tool-card-summary" title={summary}>{summary}</span>
          ) : hasBody ? (
            <span className="tool-card-summary is-empty">点击展开参数与输出</span>
          ) : null}
          <span className="tool-card-meta">
            {duration ? <span className="tool-duration">{duration}</span> : null}
            {isRunning ? (
              <span className={classNames('tool-live', isStale && 'is-stale')}>
                {isStale ? '可能卡住' : '进行中'}
              </span>
            ) : null}
            {hasBody ? (
              <ChevronRight
                aria-hidden="true"
                className={classNames('tool-card-chevron', cardOpen && 'is-open')}
                size={14}
              />
            ) : null}
          </span>
        </span>
      </button>

      {cardOpen && hasBody ? (
        <div className="tool-card-body" data-testid="tool-card-body">
          {isStale ? (
            <div className="tool-stale-note">
              已运行 {duration || '较久'}，超过网络工具超时阈值时会自动失败
            </div>
          ) : null}
          <ToolDetail
            open={argsOpen}
            onToggle={() => setArgsOpen((current) => !current)}
            testId="tool-args"
            text={argsText}
            title="参数"
          />
          {errorText ? (
            <div className="tool-error-line" data-testid="tool-error">
              {errorText}
            </div>
          ) : null}
          <ToolDetail
            open={outputOpen}
            onToggle={() => setOutputOpen((current) => !current)}
            testId="tool-output"
            text={outputText}
            title="输出"
          />
        </div>
      ) : null}
    </article>
  );
}
