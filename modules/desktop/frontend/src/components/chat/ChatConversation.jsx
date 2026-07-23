import { ArrowDown, Bot, UserRound } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  buildConversationTimeline,
  buildToolCallIndexMap,
} from '../../lib/conversationTimeline.js';
import { classNames, formatSeq } from '../../lib/format.js';
import { isWorkerToolFallback } from '../../lib/toolResultDisplay.js';
import {
  messageHasToolCallMarkup,
  parseMessageContent,
  toolItemFromMessageSegment,
} from '../../lib/messageContent.js';
import { PermissionCard } from '../PermissionCard.jsx';
import { ToolCallCard } from '../ToolCallCard.jsx';
import { Button } from '../ui/button.jsx';
import { EmptyState } from '../ui/feedback.jsx';
import { Markdown } from '../ui/Markdown.jsx';
import { RunningPanda } from '../ui/RunningPanda.jsx';

function displayMessageAgent(message) {
  if (message.agentLabel) return message.agentLabel;
  const agent = message.agent || message.role;
  if (message.role === 'user') return '我';
  if (agent === 'gateway' || agent === 'system') return '系统';
  if (message.workerId || message.profileKey) {
    const worker = message.workerId || '未知 Worker';
    return message.profileKey ? `Worker ${worker} · ${message.profileKey}` : `Worker ${worker}`;
  }
  if (agent === 'assistant') return '助手';
  return agent;
}

/** Render assistant message body and convert embedded tool markup into cards. */
function AssistantMessageBody({
  message,
  workspaceRoot = '',
  toolIndexById,
  toolTotal = 0,
}) {
  const text = message.text || '';
  if (isWorkerToolFallback(message)) {
    return (
      <div className="message-recovery-notice" data-testid="message-recovery-notice">
        Worker 未生成可用的最终报告。完整执行结果仍保留在工具卡片中。
      </div>
    );
  }
  const segments = useMemo(() => {
    if (!messageHasToolCallMarkup(text)) {
      return [{ type: 'text', text }];
    }
    return parseMessageContent(text);
  }, [text]);

  const hasTool = segments.some((segment) => segment.type === 'tool_call');
  if (!hasTool) {
    return <Markdown className="message-markdown" workspaceRoot={workspaceRoot}>{text}</Markdown>;
  }

  const inlineToolTotal = segments.filter((segment) => segment.type === 'tool_call').length;
  let toolIndex = 0;
  return (
    <div className="message-rich-body" data-testid="message-rich-body">
      {segments.map((segment, index) => {
        if (segment.type === 'tool_call') {
          const item = toolItemFromMessageSegment(segment, message, toolIndex);
          toolIndex += 1;
          const mappedIndex = toolIndexById?.get(String(item.id));
          const callIndex = mappedIndex || toolIndex;
          const callTotal = mappedIndex ? toolTotal : inlineToolTotal;
          return (
            <div className="message-inline-tool" key={`${item.id}:${index}`}>
              <ToolCallCard callIndex={callIndex} callTotal={callTotal} item={item} />
            </div>
          );
        }
        if (!String(segment.text || '').trim()) {
          return null;
        }
        return (
          <Markdown className="message-markdown" key={`text:${index}`} workspaceRoot={workspaceRoot}>
            {segment.text}
          </Markdown>
        );
      })}
    </div>
  );
}

export function ChatConversation({
  messages,
  permissions,
  tools,
  running = false,
  workspaceRoot = '',
  onResolvePermission,
  emptyTitle = '准备开始',
}) {
  const viewportRef = useRef(null);
  const [followingLatest, setFollowingLatest] = useState(true);
  const timeline = useMemo(
    () => buildConversationTimeline(messages, tools, permissions),
    [messages, permissions, tools],
  );
  const toolIndexById = useMemo(() => buildToolCallIndexMap(tools), [tools]);
  const toolTotal = tools.length;

  useEffect(() => {
    if (!followingLatest) return undefined;
    const frame = window.requestAnimationFrame(() => {
      const viewport = viewportRef.current;
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    });
    return () => window.cancelAnimationFrame(frame);
  }, [followingLatest, timeline, running]);

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
          <EmptyState className="empty-conversation" title={emptyTitle} />
        ) : null}

        {timeline.map((item) => {
          if (item.type === 'tool') {
            const tool = item.value;
            const callIndex = toolIndexById.get(String(tool.id));
            return (
              <ToolCallCard
                callIndex={callIndex}
                callTotal={toolTotal}
                item={tool}
                key={item.key}
              />
            );
          }
          if (item.type === 'permission') {
            return <PermissionCard item={item.value} key={item.key} onResolve={onResolvePermission} />;
          }
          const message = item.value;
          const isUser = message.role === 'user';
          const avatar = (
            <div className="message-avatar" aria-hidden="true">
              {isUser ? <UserRound size={14} /> : <Bot size={14} />}
            </div>
          );
          const bubble = (
            <div className="message-bubble">
              <div className="message-meta">
                <strong>{displayMessageAgent(message)}</strong>
                {message.runSeq ? <span title={`事件 ${formatSeq(message.runSeq)}`}>{formatSeq(message.runSeq)}</span> : null}
                {!message.runSeq && message.messageSeq ? <span title={`消息 ${formatSeq(message.messageSeq)}`}>{formatSeq(message.messageSeq)}</span> : null}
              </div>
              {isUser ? (
                <p className="message-plain">{message.text}</p>
              ) : (
                <AssistantMessageBody
                  message={message}
                  toolIndexById={toolIndexById}
                  toolTotal={toolTotal}
                  workspaceRoot={workspaceRoot}
                />
              )}
            </div>
          );
          return (
            <article
              className={classNames('message-row', `role-${message.role}`)}
              data-testid="message-row"
              data-timeline-type="message"
              key={item.key}
            >
              {isUser ? (
                <>
                  {bubble}
                  {avatar}
                </>
              ) : (
                <>
                  {avatar}
                  {bubble}
                </>
              )}
            </article>
          );
        })}

        {running ? (
          <div className="running-panda-row" data-testid="running-panda-row">
            <RunningPanda label="小熊猫思考中" variant="bubble" />
          </div>
        ) : null}
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
