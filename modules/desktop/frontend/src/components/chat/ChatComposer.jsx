import { Send, Square } from 'lucide-react';
import { useEffect, useMemo, useRef } from 'react';
import { classNames } from '../../lib/format.js';
import { IconButton } from '../ui/button.jsx';
import { RunningPanda } from '../ui/RunningPanda.jsx';
import { SelectMenu } from '../ui/select.jsx';

const MIN_COMPOSER_HEIGHT = 72;
const MAX_COMPOSER_HEIGHT = 220;

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
 * 聊天输入器：固定在对话区底部，内嵌供应商模型选择与发送/取消。
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
}) {
  const textareaRef = useRef(null);
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

  function submit(event) {
    event?.preventDefault?.();
    if (!canSend) return;
    onSend();
  }

  function handleKeyDown(event) {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      submit();
    }
  }

  return (
    <form
      aria-label="任务输入"
      className={classNames('chat-composer', running && 'is-running')}
      onSubmit={submit}
    >
      <div className="composer-shell">
        <textarea
          aria-label="输入任务"
          data-testid="chat-composer-input"
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={running ? '运行中，可继续编辑下一条任务…' : '描述任务…'}
          ref={textareaRef}
          rows={1}
          value={value}
        />
        <div className="composer-toolbar">
          <div className="composer-toolbar-left">
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
              {running ? '运行中 · Enter 换行' : `Enter 换行 · ${shortcutHint}`}
            </span>
          </div>
          <div className="composer-actions">
            {running ? (
              <>
                <RunningPanda className="composer-running-panda" label="运行中" size="sm" />
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
