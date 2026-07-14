import { ShieldAlert } from 'lucide-react';
import { displayRisk, displayStatus } from '../lib/displayLabels.js';
import { Badge, StatusBadge } from './ui/badge.jsx';
import { Button } from './ui/button.jsx';

function permissionTarget(argumentsValue) {
  const args = argumentsValue || {};
  const key = ['path', 'target', 'destination', 'url', 'command'].find((name) => args[name]);
  if (!key) return '未指定';
  const value = String(args[key]);
  return value.length > 120 ? `${value.slice(0, 117)}...` : value;
}

export function PermissionCard({ item, onResolve }) {
  const resolved = item.status && item.status !== 'pending';
  const risk = item.risk || 'unknown';
  const args = item.arguments && Object.keys(item.arguments).length > 0
    ? JSON.stringify(item.arguments, null, 2)
    : '';
  const decisionLabel = item.decision === 'approve'
    ? '已允许'
    : item.decision === 'deny'
      ? '已拒绝'
      : displayStatus(item.status);

  return (
    <article
      className={`permission-card permission-${item.decision || item.status || 'pending'} permission-risk-${risk}`}
      data-testid="permission-card"
      data-timeline-type="permission"
    >
      <div className="permission-title">
        <ShieldAlert size={16} />
        <strong>{item.summary}</strong>
        <Badge tone={risk === 'high' || risk === 'critical' ? 'danger' : 'warning'}>
          {displayRisk(risk)}风险
        </Badge>
      </div>
      <p>{item.detail}</p>
      <dl className="permission-context">
        <div><dt>工具</dt><dd>{item.toolName || '未指定'}</dd></div>
        <div><dt>来源</dt><dd>{item.agent || '当前运行'}</dd></div>
        <div><dt>目标</dt><dd title={permissionTarget(item.arguments)}>{permissionTarget(item.arguments)}</dd></div>
      </dl>
      {args ? (
        <details className="permission-arguments">
          <summary>查看参数</summary>
          <pre>{args}</pre>
        </details>
      ) : null}
      {resolved || !onResolve ? (
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
          <Button data-testid="permission-approve" onClick={() => onResolve(item.id, 'approve')} variant="default">允许一次</Button>
        </div>
      )}
    </article>
  );
}
