import { Bot, CircleStop, Loader2, MessageSquare } from 'lucide-react';
import { displayAgentName, displaySubAgentBackend } from '../lib/displayLabels.js';
import { classNames, formatSeq } from '../lib/format.js';
import { StatusBadge } from './ui/badge.jsx';
import { IconButton } from './ui/button.jsx';
import { PanelHeader } from './ui/panel.jsx';

function isActiveStatus(status) {
  return ['queued', 'starting', 'running', 'cancelling', 'waiting_permission'].includes(status);
}

export function SubAgentPanel({ agents, onCancelSubAgent, onOpenSubagentConversation }) {
  return (
    <section className="subagent-panel-content">
      <PanelHeader title="子代理" />
      <div className="subagent-list">
        {agents.map((agent) => {
          const isSubAgent = agent.role === 'subagent';
          const status = agent.status || (isSubAgent ? 'running' : 'idle');
          const active = isActiveStatus(status);
          const canCancel = isSubAgent && isActiveStatus(status) && status !== 'cancelling' && agent.rootRunId;
          return (
            <article
              className={classNames('subagent-item', active && 'is-running', `status-${status}`)}
              data-status={status}
              data-testid="subagent-item"
              key={agent.id}
            >
              <span className={classNames('subagent-icon', active && 'is-active')} aria-hidden="true">
                {active ? <Loader2 className="subagent-spin" size={15} /> : <Bot size={16} />}
              </span>
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
                <span className="subagent-heading">
                  <strong>{displayAgentName(agent.name || agent.id)}</strong>
                  <StatusBadge status={status} />
                </span>
                <span className="subagent-meta">事件 {formatSeq(agent.seq)}</span>
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
