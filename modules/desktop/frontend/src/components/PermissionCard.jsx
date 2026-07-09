import { ShieldAlert } from 'lucide-react';
import { displayStatus } from '../lib/displayLabels.js';
import { StatusBadge } from './ui/badge.jsx';
import { Button } from './ui/button.jsx';

export function PermissionCard({ item, onResolve }) {
  const resolved = item.status && item.status !== 'pending';
  const decisionLabel = item.decision === 'approve'
    ? '已允许'
    : item.decision === 'deny'
      ? '已拒绝'
      : displayStatus(item.status);

  return (
    <article className={`permission-card permission-${item.decision || item.status || 'pending'}`} data-testid="permission-card">
      <div className="permission-title">
        <ShieldAlert size={16} />
        <strong>{item.summary}</strong>
      </div>
      <p>{item.detail}</p>
      {resolved ? (
        <StatusBadge
          className="permission-state"
          data-testid="permission-state"
          status={item.decision || item.status}
        >
          {decisionLabel}
        </StatusBadge>
      ) : (
        <div className="permission-actions">
          <Button data-testid="permission-deny" onClick={() => onResolve(item.id, 'deny')} variant="ghost">拒绝</Button>
          <Button data-testid="permission-approve" onClick={() => onResolve(item.id, 'approve')} variant="default">允许</Button>
        </div>
      )}
    </article>
  );
}
