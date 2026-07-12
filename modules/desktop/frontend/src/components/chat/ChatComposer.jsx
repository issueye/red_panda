import { Send, Square, Terminal } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { listCommands } from '../../lib/commands.js';
import { classNames } from '../../lib/format.js';
import { formatTokenCount } from '../../lib/tokenBudget.js';
import { IconButton } from '../ui/button.jsx';
import { SelectMenu } from '../ui/select.jsx';
import { CommandPalette } from './CommandPalette.jsx';

const MIN_COMPOSER_HEIGHT = 72;
const MAX_COMPOSER_HEIGHT = 220;
const RING_RADIUS = 9;

/**
 * 自适应调整输入框高度，限制在最小与最大行高之间。
 * @param {HTMLTextAreaElement | null} element 目标文本域
 */
function resizeComposer(element) {
  if (!element) return;
  element.style.height = '0px';
  const nextHeight = Math.min(MAX_COMPOSER_HEIGHT, Math.max(MIN_COMPOSER_HEIGHT, element.scrollHeight));
  element.style.height = `${nextHeight}px`;
  element.style.overflowY = element.scrollHeight > MAX_COMPOSER_HEIGHT ? 'auto' : 'hidden';
}

/**
 * Context budget progress ring (left of send).
 * Uses pathLength=100 so stroke-dash* is reliable across browsers.
 */
function TokenProgressRing({
  ratio = 0,
  displayRatio = 0,
  used = 0,
  maxTokens = 0,
  enabled = false,
  softBudget = false,
}) {
  const fill = Math.min(1, Math.max(0, Number(displayRatio || ratio) || 0));
  const trueRatio = Math.min(1, Math.max(0, Number(ratio) || 0));
  // pathLength=100 → dasharray 100, dashoffset (1-fill)*100
  const offset = 100 * (1 - fill);
  const percent = Math.round(trueRatio * 100);
  const tone = enabled
    ? (trueRatio >= 0.9 ? 'danger' : trueRatio >= 0.75 ? 'warn' : 'ok')
    : (used > 0 ? 'soft' : 'idle');
  const title = enabled
    ? `上下文约 ${formatTokenCount(used)} / ${formatTokenCount(maxTokens)}（${percent}%）${trueRatio >= 0.9 ? ' · 将自动更新摘要' : ''}`
    : softBudget
      ? `估算上下文约 ${formatTokenCount(used)} Token（未设置上限；在供应商配置中填写「最大 Token 数」以启用预算与自动摘要）`
      : '未设置最大 Token：在供应商配置中填写上下文窗口';
  const label = enabled
    ? `${percent}`
    : (used > 0 ? formatTokenCount(used) : '—');

  return (
    <div
      aria-label={title}
      className={classNames('composer-token-ring', `is-${tone}`)}
      data-testid="composer-token-ring"
      role="meter"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={enabled ? percent : Math.round(fill * 100)}
      title={title}
    >
      <svg className="composer-token-ring-svg" viewBox="0 0 24 24" aria-hidden="true">
        <circle
          className="composer-token-ring-track"
          cx="12"
          cy="12"
          pathLength="100"
          r={RING_RADIUS}
        />
        <circle
          className="composer-token-ring-progress"
          cx="12"
          cy="12"
          pathLength="100"
          r={RING_RADIUS}
          style={{
            strokeDasharray: 100,
            strokeDashoffset: offset,
          }}
        />
      </svg>
      <span className="composer-token-ring-label">
        {label}
      </span>
    </div>
  );
}

/**
 * 聊天输入器：固定在对话区底部，内嵌供应商模型选择与发送/取消。
 * 点击模型选择器左侧的指令按钮时弹出指令面板。
 */
export function ChatComposer({
  value,
  running,
  onChange,
  onSend,
  onCancel,
  providerProfiles = [],
  providerProfileId = '',
  onProviderProfileChange,
  tokenUsed = 0,
  tokenMax = 0,
  tokenRatio = 0,
  tokenDisplayRatio = 0,
  tokenBudgetEnabled = false,
  tokenSoftBudget = false,
}) {
  const composerRef = useRef(null);
  const shellRef = useRef(null);
  const textareaRef = useRef(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);

  const suggestions = useMemo(() => listCommands(), []);

  const canSend = value.trim().length > 0 && !running;
  const shortcutHint = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || '')
    ? '⌘ + Enter 发送'
    : 'Ctrl + Enter 发送';

  const providerOptions = useMemo(() => {
    const active = providerProfiles.filter((item) => item.active !== false);
    const options = [
      { value: '', label: '默认模型' },
      ...active.map((item) => ({
        value: item.id,
        label: item.model ? `${item.name} · ${item.model}` : item.name,
      })),
    ];
    if (providerProfileId && !options.some((item) => item.value === providerProfileId)) {
      const missing = providerProfiles.find((item) => item.id === providerProfileId);
      if (missing) {
        options.push({
          value: missing.id,
          label: missing.model ? `${missing.name} · ${missing.model}` : missing.name,
        });
      }
    }
    return options;
  }, [providerProfiles, providerProfileId]);

  useEffect(() => {
    resizeComposer(textareaRef.current);
  }, [value]);

  useEffect(() => {
    if (!paletteOpen) return undefined;
    const handlePointerDown = (event) => {
      const target = event.target;
      if (composerRef.current?.contains(target)) return;
      if (target instanceof Element && target.closest('#composer-command-panel')) return;
      setPaletteOpen(false);
    };
    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [paletteOpen]);

  function togglePalette() {
    setActiveIndex(0);
    setPaletteOpen((open) => !open);
    requestAnimationFrame(() => textareaRef.current?.focus());
  }

  function applyCommand(item) {
    if (!item) return;
    const next = item.insert || item.usage || `/${item.name}`;
    onChange?.(next);
    setPaletteOpen(false);
    requestAnimationFrame(() => {
      const el = textareaRef.current;
      if (!el) return;
      el.focus();
      const pos = next.length;
      try {
        el.setSelectionRange(pos, pos);
      } catch {
        // ignore
      }
    });
  }

  function submit(event) {
    event?.preventDefault?.();
    if (!canSend) return;
    onSend();
  }

  function handleKeyDown(event) {
    if (paletteOpen) {
      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setActiveIndex((i) => (i + 1) % suggestions.length);
        return;
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault();
        setActiveIndex((i) => (i - 1 + suggestions.length) % suggestions.length);
        return;
      }
      if (event.key === 'Tab' || (event.key === 'Enter' && !event.ctrlKey && !event.metaKey && !event.shiftKey)) {
        event.preventDefault();
        applyCommand(suggestions[activeIndex] || suggestions[0]);
        return;
      }
      if (event.key === 'Escape') {
        event.preventDefault();
        setPaletteOpen(false);
        return;
      }
    }

    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    }
  }

  function handleChange(event) {
    onChange?.(event.target.value);
  }

  return (
    <form
      aria-label="任务输入"
      className={classNames('chat-composer', running && 'is-running', paletteOpen && 'has-command-panel')}
      onSubmit={submit}
      ref={composerRef}
    >
      <div className="composer-shell" ref={shellRef}>
        <CommandPalette
          activeIndex={activeIndex}
          anchorRef={shellRef}
          items={suggestions}
          onHover={setActiveIndex}
          onSelect={applyCommand}
          visible={paletteOpen}
        />
        <textarea
          aria-autocomplete={paletteOpen ? 'list' : undefined}
          aria-controls={paletteOpen ? 'composer-command-panel' : undefined}
          aria-expanded={paletteOpen || undefined}
          aria-label="输入任务"
          data-testid="chat-composer-input"
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          placeholder={running ? '运行中，可继续编辑下一条任务…' : '描述任务…'}
          ref={textareaRef}
          rows={1}
          value={value}
        />
        <div className="composer-toolbar">
          <div className="composer-toolbar-left">
            <IconButton
              aria-controls="composer-command-panel"
              aria-expanded={paletteOpen}
              aria-haspopup="listbox"
              className="composer-command-trigger"
              data-testid="composer-command-trigger"
              label={paletteOpen ? '关闭指令' : '选择指令'}
              onClick={togglePalette}
              type="button"
              variant={paletteOpen ? 'soft' : 'ghost'}
            >
              <Terminal size={15} />
            </IconButton>
            <SelectMenu
              ariaLabel="选择供应商模型"
              className="composer-provider-select"
              disabled={running}
              onChange={(next) => onProviderProfileChange?.(next)}
              options={providerOptions}
              placement="top"
              testId="composer-provider-select"
              value={providerProfileId || ''}
            />
            <span className="composer-hint">
              {paletteOpen
                ? '指令模式 · ↑↓ 选择 · Tab 填入'
                : running
                  ? '运行中 · Enter 换行'
                  : `Enter 换行 · ${shortcutHint}`}
            </span>
          </div>
          <div className="composer-actions">
            <TokenProgressRing
              displayRatio={tokenDisplayRatio || tokenRatio}
              enabled={tokenBudgetEnabled}
              maxTokens={tokenMax}
              ratio={tokenRatio}
              softBudget={tokenSoftBudget}
              used={tokenUsed}
            />
            {running ? (
              <>
                <span className="composer-running-status">运行中</span>
                <IconButton
                  className="composer-action composer-cancel"
                  data-testid="chat-composer-cancel"
                  label="取消运行"
                  onClick={onCancel}
                  type="button"
                  variant="soft"
                >
                  <Square size={14} />
                </IconButton>
              </>
            ) : (
              <IconButton
                className="composer-action composer-send"
                data-testid="chat-composer-send"
                disabled={!canSend}
                label="发送"
                type="submit"
                variant="default"
              >
                <Send size={15} />
              </IconButton>
            )}
          </div>
        </div>
      </div>
    </form>
  );
}
