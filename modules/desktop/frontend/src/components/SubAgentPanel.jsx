import { Bot, CircleStop } from 'lucide-react';
import { displaySubAgentBackend } from '../lib/displayLabels.js';
import { formatSeq } from '../lib/format.js';
import { displayStatus } from './ui/badge.jsx';
import { IconButton } from './ui/button.jsx';
import { PanelHeader } from './ui/panel.jsx';

export function SubAgentPanel({ agents, onCancelSubAgent }) {
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
              <div>
                <strong>{agent.name}</strong>
                <span>{displayStatus(agent.status)} - seq {formatSeq(agent.seq)}</span>
                {agent.backend ? <em>{displaySubAgentBackend(agent.backend)}</em> : null}
                {agent.summary ? <small>{agent.summary}</small> : null}
              </div>
              {isSubAgent ? (
                <IconButton
                  data-testid="subagent-cancel"
                  disabled={!canCancel}
                  label={canCancel ? '取消子代理' : '当前子代理不可取消'}
                  onClick={() => onCancelSubAgent?.(agent)}
                >
                  <CircleStop size={15} />
                </IconButton>
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
