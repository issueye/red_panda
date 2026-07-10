import { ArrowDown, Bot, UserRound } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { buildConversationTimeline } from '../../lib/conversationTimeline.js';
import { classNames, formatSeq } from '../../lib/format.js';
import { PermissionCard } from '../PermissionCard.jsx';
import { ToolCallCard } from '../ToolCallCard.jsx';
import { Button } from '../ui/button.jsx';
import { EmptyState } from '../ui/feedback.jsx';

function displayMessageAgent(message) {
  const agent = message.agent || message.role;
  if (message.role === 'user') return '我';
  if (agent === 'root') return '主代理';
  if (agent === 'gateway' || agent === 'system') return '系统';
  if (agent === 'assistant') return '助手';
  if (agent === 'subagent') return '子代理';
  return agent;
}

export function ChatConversation({ messages, permissions, tools, onResolvePermission }) {
  const viewportRef = useRef(null);
  const [followingLatest, setFollowingLatest] = useState(true);
  const timeline = useMemo(
    () => buildConversationTimeline(messages, tools, permissions),
    [messages, permissions, tools],
  );

  useEffect(() => {
    if (!followingLatest) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const viewport = viewportRef.current;
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [followingLatest, timeline]);

  function handleScroll() {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
    setFollowingLatest(distanceFromBottom <= 48);
  }

  function goToLatest() {
    setFollowingLatest(true);
    const viewport = viewportRef.current;
    if (viewport) viewport.scrollTop = viewport.scrollHeight;
  }

  return (
    <div className="conversation-shell">
      <div
        className="conversation"
        data-testid="chat-conversation"
        onScroll={handleScroll}
        ref={viewportRef}
      >
        {timeline.length === 0 ? (
          <EmptyState className="empty-conversation" title="准备开始" />
        ) : null}

        {timeline.map((item) => {
          if (item.type === 'tool') {
            return <ToolCallCard item={item.value} key={item.key} />;
          }
          if (item.type === 'permission') {
            return <PermissionCard item={item.value} key={item.key} onResolve={onResolvePermission} />;
          }
          const message = item.value;
          return (
            <article
              className={classNames('message-row', `role-${message.role}`)}
              data-testid="message-row"
              data-timeline-type="message"
              key={item.key}
            >
              <div className="message-avatar">
                {message.role === 'user' ? <UserRound size={16} /> : <Bot size={16} />}
              </div>
              <div className="message-bubble">
                <div className="message-meta">
                  <strong>{displayMessageAgent(message)}</strong>
                  {message.rootSeq ? <span>事件 {formatSeq(message.rootSeq)}</span> : null}
                  {!message.rootSeq && message.messageSeq ? <span>消息 {formatSeq(message.messageSeq)}</span> : null}
                </div>
                <p>{message.text}</p>
              </div>
            </article>
          );
        })}
      </div>
      {!followingLatest && timeline.length > 0 ? (
        <Button
          className="conversation-latest"
          data-testid="conversation-latest"
          icon={<ArrowDown size={14} />}
          onClick={goToLatest}
          variant="soft"
        >
          回到最新
        </Button>
      ) : null}
    </div>
  );
}
