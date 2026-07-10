import { CheckCircle2, Loader2, Wrench, XCircle } from 'lucide-react';
import { displayRisk } from '../lib/displayLabels.js';
import { StatusBadge } from './ui/badge.jsx';

function statusIcon(status) {
  if (status === 'completed') return <CheckCircle2 size={15} />;
  if (status === 'failed' || status === 'denied') return <XCircle size={15} />;
  return <Loader2 size={15} />;
}

export function ToolCallCard({ item }) {
  const args = item.arguments ? JSON.stringify(item.arguments, null, 2) : '';
  return (
    <article className={`tool-card tool-${item.status || 'running'}`} data-testid="tool-card" data-timeline-type="tool">
      <div className="tool-title">
        <Wrench size={16} />
        <div>
          <strong>{item.displayName || item.name}</strong>
          <span>{item.name} - {displayRisk(item.risk || 'low')}风险</span>
        </div>
        <StatusBadge
          className="tool-status"
          data-testid="tool-status"
          icon={statusIcon(item.status)}
          status={item.status || 'running'}
        />
      </div>
      {args ? <pre className="tool-args">{args}</pre> : null}
      {item.output ? <pre className="tool-output">{item.output}</pre> : null}
      {item.error ? <p className="tool-error">{item.error}</p> : null}
    </article>
  );
}
