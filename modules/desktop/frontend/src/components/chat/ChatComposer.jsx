import { Send, Square } from 'lucide-react';
import { useEffect, useRef } from 'react';
import { classNames } from '../../lib/format.js';
import { IconButton } from '../ui/button.jsx';

const MIN_COMPOSER_HEIGHT = 24;
const MAX_COMPOSER_HEIGHT = 168;

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
 * 聊天输入器：固定在对话区底部，发送/取消按钮内嵌在输入壳内。
 * @param {{ value: string, running: boolean, onChange: (value: string) => void, onSend: () => void, onCancel: () => void }} props 组件属性
 */
export function ChatComposer({ value, running, onChange, onSend, onCancel }) {
  const textareaRef = useRef(null);
  const canSend = value.trim().length > 0 && !running;
  const shortcutHint = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || '')
    ? '⌘ + Enter 发送'
    : 'Ctrl + Enter 发送';

  useEffect(() => {
    resizeComposer(textareaRef.current);
  }, [value]);

  /**
   * 提交当前草稿；空内容或运行中时忽略。
   * @param {Event} [event] 表单提交事件
   */
  function submit(event) {
    event?.preventDefault?.();
    if (!canSend) return;
    onSend();
  }

  /**
   * 处理快捷键：Ctrl/Cmd+Enter 发送，Enter 换行。
   * @param {KeyboardEvent} event 键盘事件
   */
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
          <span className="composer-hint">
            {running ? '运行中 · Enter 换行' : `Enter 换行 · ${shortcutHint}`}
          </span>
          <div className="composer-actions">
            {running ? (
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
