import { ArrowDown, BrainCircuit, ChevronRight, Undo2, UserRound } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  buildConversationTimeline,
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
import { ToolExecutionGroup } from '../ToolExecutionGroup.jsx';
import { Button } from '../ui/button.jsx';
import { EmptyState } from '../ui/feedback.jsx';
import { Markdown } from '../ui/Markdown.jsx';
import { RunningPanda } from '../ui/RunningPanda.jsx';
import mark from '../../assets/red-panda-mark.svg';
import { AttachmentThumbs } from './AttachmentThumbs.jsx';

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
function groupInlineSegments(segments, message) {
  const grouped = [];
  let tools = [];
  function flushTools() {
    if (tools.length === 1) grouped.push({ type: 'tool', item: tools[0] });
    if (tools.length > 1) grouped.push({ type: 'tool_group', items: tools });
    tools = [];
  }
  let toolIndex = 0;
  segments.forEach((segment, index) => {
    if (segment.type === 'tool_call') {
      tools.push(toolItemFromMessageSegment(segment, message, toolIndex));
      toolIndex += 1;
      return;
    }
    flushTools();
    grouped.push({ ...segment, key: `text:${index}` });
  });
  flushTools();
  return grouped;
}

function groupTimelineTools(timeline) {
  const grouped = [];
  let tools = [];
  function flushTools() {
    if (tools.length === 1) grouped.push(tools[0]);
    if (tools.length > 1) {
      grouped.push({
        type: 'tool_group',
        key: `tool-group:${tools[0].key}`,
        values: tools.map((item) => item.value),
      });
    }
    tools = [];
  }
  timeline.forEach((item) => {
    if (item.type === 'tool') {
      tools.push(item);
      return;
    }
    flushTools();
    grouped.push(item);
  });
  flushTools();
  return grouped;
}

function AssistantMessageBody({ message, workspaceRoot = '' }) {
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

  const groupedSegments = groupInlineSegments(segments, message);
  return (
    <div className="message-rich-body" data-testid="message-rich-body">
      {groupedSegments.map((segment, index) => {
        if (segment.type === 'tool_group') {
          return (
            <div className="message-inline-tool" key={`tool-group:${segment.items[0]?.id || index}`}>
              <ToolExecutionGroup items={segment.items} />
            </div>
          );
        }
        if (segment.type === 'tool') {
          return <ToolCallCard item={segment.item} key={segment.item.id || index} />;
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
  onRollbackMessage,
  emptyTitle = '准备开始',
}) {
  const viewportRef = useRef(null);
  const [followingLatest, setFollowingLatest] = useState(true);
  const timeline = useMemo(
    () => groupTimelineTools(buildConversationTimeline(messages, tools, permissions)),
    [messages, permissions, tools],
  );

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
          <EmptyState className="empty-conversation" title={emptyTitle}>
            <img alt="" className="empty-conversation-logo" draggable={false} src={mark} />
          </EmptyState>
        ) : null}

        {timeline.map((item) => {
          if (item.type === 'tool_group') {
            return <ToolExecutionGroup items={item.values} key={item.key} />;
          }
          if (item.type === 'tool') {
            return <ToolCallCard item={item.value} key={item.key} />;
          }
          if (item.type === 'permission') {
            return <PermissionCard item={item.value} key={item.key} onResolve={onResolvePermission} />;
          }
          const message = item.value;
          const isUser = message.role === 'user';
          const isReasoning = message.role === 'reasoning';
          if (isReasoning) {
            const reasoningPreview = String(message.text || '')
              .replace(/\s+/g, ' ')
              .trim();
            const compactPreview = reasoningPreview.length > 96
              ? `${reasoningPreview.slice(0, 96)}…`
              : reasoningPreview;
            return (
              <article
                className="message-row role-reasoning"
                data-testid="reasoning-row"
                data-timeline-type="message"
                key={item.key}
              >
                <div className="message-avatar" aria-hidden="true">
                  <BrainCircuit size={14} />
                </div>
                <details className="message-thinking" data-testid="reasoning-block">
                  <summary>
                    <span className="message-thinking-heading">
                      <strong>思考</strong>
                    </span>
                    {compactPreview ? (
                      <span className="message-thinking-preview" title={compactPreview}>
                        {compactPreview}
                      </span>
                    ) : null}
                    <span className="message-thinking-action" aria-hidden="true">
                      <span className="message-thinking-action-open">查看</span>
                      <span className="message-thinking-action-close">收起</span>
                    </span>
                    <ChevronRight aria-hidden="true" className="message-thinking-chevron" size={13} />
                  </summary>
                  <Markdown className="message-thinking-content" workspaceRoot={workspaceRoot}>
                    {message.text}
                  </Markdown>
                </details>
              </article>
            );
          }
          const avatar = isUser ? (
            <div className="message-avatar" aria-hidden="true">
              <UserRound size={14} />
            </div>
          ) : null;
          const bubble = (
            <div className="message-bubble">
              <div className="message-meta">
                <strong>{displayMessageAgent(message)}</strong>
                {message.runSeq ? <span title={`事件 ${formatSeq(message.runSeq)}`}>{formatSeq(message.runSeq)}</span> : null}
                {!message.runSeq && message.messageSeq ? <span title={`消息 ${formatSeq(message.messageSeq)}`}>{formatSeq(message.messageSeq)}</span> : null}
                {isUser && onRollbackMessage && message.messageSeq ? (
                  <button
                    className="message-rollback"
                    data-testid="message-rollback"
                    onClick={() => onRollbackMessage(message)}
                    title="回滚到此消息并重新编辑"
                    type="button"
                  >
                    <Undo2 aria-hidden="true" size={13} />
                    <span>回滚</span>
                  </button>
                ) : null}
              </div>
              {isUser ? (
                <>
                  <AttachmentThumbs attachments={message.attachments} />
                  <p className="message-plain">{message.text}</p>
                </>
              ) : (
                <AssistantMessageBody
                  message={message}
                  workspaceRoot={workspaceRoot}
                />
              )}
            </div>
          );
          return (
            <article
              className={classNames(
                'message-row',
                `role-${message.role}`,
                !isUser && 'message-row-no-avatar',
              )}
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
                bubble
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
