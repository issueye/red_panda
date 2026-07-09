import { Bot, CircleStop } from 'lucide-react';
import { formatSeq } from '../lib/format.js';
import { IconButton } from './ui/button.jsx';

export function SubAgentPanel({ agents, onCancelSubAgent }) {
  return (
    <section className="subagent-panel-content">
      <div className="panel-header">
        <div>
          <strong>Subagents</strong>
          <span>Shared root run event channel</span>
        </div>
      </div>
      <div className="subagent-list">
        {agents.map((agent) => {
          const isSubAgent = agent.role === 'subagent';
          const canCancel = isSubAgent && agent.status === 'running' && agent.rootRunId;
          return (
            <article className="subagent-item" data-testid="subagent-item" key={agent.id}>
              <Bot size={16} />
              <div>
                <strong>{agent.name}</strong>
                <span>{agent.status} - seq {formatSeq(agent.seq)}</span>
                {agent.backend ? <em>{agent.backend}</em> : null}
                {agent.summary ? <small>{agent.summary}</small> : null}
              </div>
              {isSubAgent ? (
                <IconButton
                  data-testid="subagent-cancel"
                  disabled={!canCancel}
                  label={canCancel ? 'Cancel subagent' : 'Subagent cannot be cancelled'}
                  onClick={() => onCancelSubAgent?.(agent)}
                >
                  <CircleStop size={15} />
                </IconButton>
              ) : (
                <span className="subagent-root-marker">Root</span>
              )}
            </article>
          );
        })}
      </div>
    </section>
  );
}
