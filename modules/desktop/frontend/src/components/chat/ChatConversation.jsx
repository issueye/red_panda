import { Bot, UserRound } from 'lucide-react';
import { classNames, formatSeq } from '../../lib/format.js';
import { PermissionCard } from '../PermissionCard.jsx';
import { ToolCallCard } from '../ToolCallCard.jsx';

export function ChatConversation({ messages, permissions, tools, onResolvePermission }) {
  return (
    <div className="conversation" data-testid="chat-conversation">
      {messages.length === 0 ? (
        <div className="empty-conversation">
          <strong>Ready to start</strong>
          <span>After the gateway connects, new tasks enter the Agent Runtime through WebSocket run.start.</span>
        </div>
      ) : null}

      {messages.map((message) => (
        <article className={classNames('message-row', `role-${message.role}`)} data-testid="message-row" key={message.id}>
          <div className="message-avatar">
            {message.role === 'user' ? <UserRound size={16} /> : <Bot size={16} />}
          </div>
          <div className="message-bubble">
            <div className="message-meta">
              <strong>{message.agent || message.role}</strong>
              {message.rootSeq ? <span>root_seq {formatSeq(message.rootSeq)}</span> : null}
            </div>
            <p>{message.text}</p>
          </div>
        </article>
      ))}

      {tools.map((item) => (
        <ToolCallCard item={item} key={item.id} />
      ))}

      {permissions.map((item) => (
        <PermissionCard item={item} key={item.id} onResolve={onResolvePermission} />
      ))}
    </div>
  );
}
