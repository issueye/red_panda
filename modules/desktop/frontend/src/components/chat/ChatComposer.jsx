import { Send, Square } from 'lucide-react';
import { Button, IconButton } from '../ui/button.jsx';

export function ChatComposer({ value, running, onChange, onSend, onCancel }) {
  function submit(event) {
    event.preventDefault();
    onSend();
  }

  return (
    <form className="chat-composer" onSubmit={submit}>
      <textarea
        data-testid="chat-composer-input"
        onChange={(event) => onChange(event.target.value)}
        placeholder="输入任务..."
        rows={3}
        value={value}
      />
      <div className="composer-actions">
        {running ? (
          <IconButton data-testid="chat-composer-cancel" label="取消运行" onClick={onCancel}>
            <Square size={16} />
          </IconButton>
        ) : (
          <Button data-testid="chat-composer-send" icon={<Send size={15} />} type="submit">
            发送
          </Button>
        )}
      </div>
    </form>
  );
}
