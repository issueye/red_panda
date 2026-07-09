import { classNames } from '../../lib/format.js';
import { displayStatus } from '../../lib/displayLabels.js';

export { displayStatus };

export function Badge({ children, className, icon, tone = 'neutral', ...props }) {
  return (
    <span className={classNames('ui-badge', `ui-badge-${tone}`, className)} {...props}>
      {icon ? <span className="ui-badge-icon">{icon}</span> : null}
      <span>{children}</span>
    </span>
  );
}

const statusTone = {
  approve: 'success',
  completed: 'success',
  connected: 'success',
  denied: 'danger',
  deny: 'danger',
  disconnected: 'danger',
  error: 'danger',
  failed: 'danger',
  cancelled: 'danger',
  pending: 'warning',
  running: 'info',
  resolved: 'success',
  waiting_permission: 'warning',
};

export function StatusBadge({ children, className, icon, status = 'neutral', ...props }) {
  const normalized = status || 'neutral';
  return (
    <Badge
      className={classNames(`status-${normalized}`, className)}
      icon={icon}
      tone={statusTone[normalized] || 'neutral'}
      {...props}
    >
      {children || displayStatus(normalized)}
    </Badge>
  );
}
