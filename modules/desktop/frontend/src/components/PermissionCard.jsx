import { ShieldAlert } from 'lucide-react';
import { Button } from './ui/button.jsx';

export function PermissionCard({ item, onResolve }) {
  const resolved = item.status && item.status !== 'pending';
  const decisionLabel = item.decision === 'approve'
    ? 'Approved'
    : item.decision === 'deny'
      ? 'Denied'
      : item.status;

  return (
    <article className={`permission-card permission-${item.decision || item.status || 'pending'}`} data-testid="permission-card">
      <div className="permission-title">
        <ShieldAlert size={16} />
        <strong>{item.summary}</strong>
      </div>
      <p>{item.detail}</p>
      {resolved ? (
        <div className="permission-state" data-testid="permission-state">{decisionLabel}</div>
      ) : (
        <div className="permission-actions">
          <Button data-testid="permission-deny" onClick={() => onResolve(item.id, 'deny')} variant="ghost">Deny</Button>
          <Button data-testid="permission-approve" onClick={() => onResolve(item.id, 'approve')} variant="default">Allow</Button>
        </div>
      )}
    </article>
  );
}
