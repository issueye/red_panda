import { ImagePlus, Send, Square, Terminal } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { listCommands } from '../../lib/commands.js';
import { ATTACHMENT_MAX_PER_RUN } from '../../lib/attachments.js';
import { classNames } from '../../lib/format.js';
import { formatTokenCount } from '../../lib/tokenBudget.js';
import { IconButton } from '../ui/button.jsx';
import { AttachmentChipBar } from './AttachmentChipBar.jsx';
import { CommandPalette } from './CommandPalette.jsx';
import { ComposerModelMenu } from './ComposerModelMenu.jsx';

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
    ? (trueRatio >= 0.8 ? 'danger' : trueRatio >= 0.65 ? 'warn' : 'ok')
    : (used > 0 ? 'soft' : 'idle');
  const title = enabled
    ? `上下文约 ${formatTokenCount(used)} / ${formatTokenCount(maxTokens)}（${percent}%）${trueRatio >= 0.8 ? ' · 将自动更新摘要' : ''}`
    : softBudget
      ? `估算上下文约 ${formatTokenCount(used)} Token（未设置上限；在供应商配置中填写「最大 Token 数」以启用预算与自动摘要）`
      : '未设置最大 Token：在供应商配置中填写上下文窗口';
  // Show the changing token estimate in the centre; percentage remains in the
  // accessible label/tooltip. This avoids a static "1%" across long sessions.
  const label = used > 0 ? formatTokenCount(used) : '—';

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
          strokeDasharray="100 100"
          strokeDashoffset={offset}
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
  model = '',
  reasoningEffort = '',
  onProviderProfileChange,
  onReasoningEffortChange,
  tokenUsed = 0,
  tokenMax = 0,
  tokenRatio = 0,
  tokenDisplayRatio = 0,
  tokenBudgetEnabled = false,
  tokenSoftBudget = false,
  attachments = [],
  onAttachmentsChange,
  onAddFiles,
  uploading = false,
  enterToSend = true,
}) {
  const composerRef = useRef(null);
  const shellRef = useRef(null);
  const textareaRef = useRef(null);
  const fileInputRef = useRef(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [dragOver, setDragOver] = useState(false);

  const suggestions = useMemo(() => listCommands(), []);

  const hasAttachments = attachments.length > 0;
  // Allow send when there's text OR at least one attachment (docs/51 §8.1).
  // While attachments upload, keep the send control visible with loading so the
  // user sees progress (running switches this control to cancel instead).
  const canSend = (value.trim().length > 0 || hasAttachments) && !running && !uploading;
  const sendLoading = Boolean(uploading);
  const attachmentsDisabled = running || uploading || !onAddFiles;
  const attachmentCountLabel = hasAttachments ? `${attachments.length}/${ATTACHMENT_MAX_PER_RUN}` : '';
  const modKey = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || '')
    ? '⌘'
    : 'Ctrl';
  const shortcutHint = enterToSend
    ? 'Enter 发送 · Shift + Enter 换行'
    : `Enter 换行 · ${modKey} + Enter 发送`;

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

    if (event.key !== 'Enter') {
      return;
    }

    // IME composition: don't send/newline while composing CJK text.
    if (event.isComposing || event.keyCode === 229) {
      return;
    }

    if (enterToSend) {
      if (event.shiftKey) {
        return; // allow newline
      }
      event.preventDefault();
      submit();
      return;
    }

    if (event.ctrlKey || event.metaKey) {
      event.preventDefault();
      submit();
    }
  }

  function handleChange(event) {
    onChange?.(event.target.value);
  }

  // Collect image files from a paste / drop / picker event. Returns [] if none
  // or attachments are disabled (docs/51 §8.1).
  function collectImageFiles(items) {
    if (attachmentsDisabled) return [];
    const files = [];
    items.forEach((item) => {
      if (item.kind === 'file' && item.type.startsWith('image/')) {
        const file = item.getAsFile();
        if (file) files.push(file);
      }
    });
    return files;
  }

  function handlePaste(event) {
    if (!event.clipboardData) return;
    const files = collectImageFiles(Array.from(event.clipboardData.items));
    if (files.length) {
      event.preventDefault();
      onAddFiles?.(files);
    }
  }

  function handleDrop(event) {
    if (!event.dataTransfer) return;
    const files = collectImageFiles(Array.from(event.dataTransfer.items));
    setDragOver(false);
    if (files.length) {
      event.preventDefault();
      onAddFiles?.(files);
    }
  }

  function handleDragOver(event) {
    if (attachmentsDisabled) return;
    if (!Array.from(event.dataTransfer?.items || []).some((i) => i.kind === 'file' && i.type.startsWith('image/'))) {
      return;
    }
    event.preventDefault();
    setDragOver(true);
  }

  function handleDragLeave(event) {
    if (!composerRef.current?.contains(event.relatedTarget)) {
      setDragOver(false);
    }
  }

  function pickFiles() {
    if (attachmentsDisabled || attachments.length >= ATTACHMENT_MAX_PER_RUN) return;
    fileInputRef.current?.click();
  }

  function handleFileInputChange(event) {
    const files = Array.from(event.target.files || []);
    event.target.value = '';
    if (files.length) onAddFiles?.(files);
  }

  return (
    <form
      aria-label="任务输入"
      className={classNames('chat-composer', running && 'is-running', paletteOpen && 'has-command-panel', dragOver && 'is-drag-over')}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
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
        <input
          accept="image/png,image/jpeg,image/webp,image/gif"
          aria-label="选择图片附件"
          data-testid="chat-composer-attachment-input"
          multiple
          onChange={handleFileInputChange}
          ref={fileInputRef}
          style={{ display: 'none' }}
          type="file"
        />
        {hasAttachments || uploading ? (
          <AttachmentChipBar
            attachments={attachments}
            disabled={attachmentsDisabled}
            onChange={onAttachmentsChange}
            onAddFiles={onAddFiles}
            uploading={uploading}
          />
        ) : null}
        <textarea
          aria-autocomplete={paletteOpen ? 'list' : undefined}
          aria-controls={paletteOpen ? 'composer-command-panel' : undefined}
          aria-expanded={paletteOpen || undefined}
          aria-label="输入任务"
          data-testid="chat-composer-input"
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
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
            <IconButton
              className="composer-attachment-trigger"
              data-testid="composer-attachment-trigger"
              disabled={attachmentsDisabled || attachments.length >= ATTACHMENT_MAX_PER_RUN}
              label={attachmentCountLabel ? `图片 ${attachmentCountLabel}` : '添加图片'}
              onClick={pickFiles}
              type="button"
              variant="ghost"
            >
              <ImagePlus size={15} />
            </IconButton>
            <ComposerModelMenu
              disabled={running}
              model={model}
              onProviderProfileChange={onProviderProfileChange}
              onReasoningEffortChange={onReasoningEffortChange}
              providerProfileId={providerProfileId}
              providerProfiles={providerProfiles}
              reasoningEffort={reasoningEffort}
            />
            <span className="composer-hint">
              {paletteOpen
                ? '指令模式 · ↑↓ 选择 · Tab 填入'
                : running
                  ? (enterToSend ? '运行中 · Shift + Enter 换行' : '运行中 · Enter 换行')
                  : shortcutHint}
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
                disabled={!canSend && !sendLoading}
                label={sendLoading ? '上传附件中' : '发送'}
                loading={sendLoading}
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
