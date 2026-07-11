import { Bot, CircleStop, MessageSquare } from 'lucide-react';
import { displaySubAgentBackend } from '../lib/displayLabels.js';
import { formatSeq } from '../lib/format.js';
import { displayStatus } from './ui/badge.jsx';
import { IconButton } from './ui/button.jsx';
import { PanelHeader } from './ui/panel.jsx';

export function SubAgentPanel({ agents, onCancelSubAgent, onOpenSubagentConversation }) {
  return (
    <section className="subagent-panel-content">
      <PanelHeader title="子代理" />
      <div className="subagent-list">
        {agents.map((agent) => {
          const isSubAgent = agent.role === 'subagent';
          const canCancel = isSubAgent && agent.status === 'running' && agent.rootRunId;
          return (
            <article className="subagent-item" data-testid="subagent-item" key={agent.id}>
              <Bot size={16} />
              <button
                className="subagent-main"
                data-testid={isSubAgent ? 'subagent-open' : undefined}
                disabled={!isSubAgent}
                onClick={() => {
                  if (isSubAgent) onOpenSubagentConversation?.(agent);
                }}
                title={isSubAgent ? '在对话标签中打开' : undefined}
                type="button"
              >
                <strong>{agent.name}</strong>
                <span>{displayStatus(agent.status)} - 事件 {formatSeq(agent.seq)}</span>
                {agent.backend ? <em>{displaySubAgentBackend(agent.backend)}</em> : null}
                {agent.summary ? <small>{agent.summary}</small> : null}
              </button>
              {isSubAgent ? (
                <div className="subagent-actions">
                  <IconButton
                    data-testid="subagent-open-tab"
                    label="打开对话"
                    onClick={() => onOpenSubagentConversation?.(agent)}
                  >
                    <MessageSquare size={14} />
                  </IconButton>
                  <IconButton
                    data-testid="subagent-cancel"
                    disabled={!canCancel}
                    label={canCancel ? '取消子代理' : '当前子代理不可取消'}
                    onClick={() => onCancelSubAgent?.(agent)}
                  >
                    <CircleStop size={15} />
                  </IconButton>
                </div>
              ) : (
                <span className="subagent-root-marker">根</span>
              )}
            </article>
          );
        })}
      </div>
    </section>
  );
}
