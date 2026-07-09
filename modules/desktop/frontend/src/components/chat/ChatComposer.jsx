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
        placeholder="Type a task and send it through the gateway WebSocket..."
        rows={3}
        value={value}
      />
      <div className="composer-actions">
        <span>Main agent and subagent output is merged by root_seq.</span>
        {running ? (
          <IconButton data-testid="chat-composer-cancel" label="Cancel run" onClick={onCancel}>
            <Square size={16} />
          </IconButton>
        ) : (
          <Button data-testid="chat-composer-send" icon={<Send size={15} />} type="submit">
            Send
          </Button>
        )}
      </div>
    </form>
  );
}
