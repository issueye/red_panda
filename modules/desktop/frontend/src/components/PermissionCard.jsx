import { useState } from 'react';
import { ShieldAlert } from 'lucide-react';
import { displayRisk, displayStatus } from '../lib/displayLabels.js';
import { classNames } from '../lib/format.js';
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
  const [resolving, setResolving] = useState('');
  const resolved = item.status && item.status !== 'pending';
  const risk = item.risk || 'unknown';
  const isHighRisk = risk === 'high' || risk === 'critical';
  const pending = !resolved && Boolean(onResolve);
  const args = item.arguments && Object.keys(item.arguments).length > 0
    ? JSON.stringify(item.arguments, null, 2)
    : '';
  const decisionLabel = item.decision === 'approve'
    ? '已允许'
    : item.decision === 'deny'
      ? '已拒绝'
      : displayStatus(item.status);

  async function handleResolve(decision) {
    if (!onResolve || resolving) return;
    setResolving(decision);
    try {
      await onResolve(item.id, decision);
    } finally {
      setResolving('');
    }
  }

  return (
    <article
      className={classNames(
        'permission-card',
        `permission-${item.decision || item.status || 'pending'}`,
        `permission-risk-${risk}`,
        pending && 'is-pending',
        pending && isHighRisk && 'is-high-risk-pending',
      )}
      data-testid="permission-card"
      data-timeline-type="permission"
    >
      <div className="permission-title">
        <ShieldAlert size={16} />
        <strong>{item.summary}</strong>
        <Badge tone={isHighRisk ? 'danger' : 'warning'}>
          {displayRisk(risk)}风险
        </Badge>
        {pending ? (
          <Badge className="permission-pending-badge" tone="warning">
            待决策
          </Badge>
        ) : null}
      </div>
      <p>{item.detail}</p>
      <dl className="permission-context">
        <div><dt>工具</dt><dd>{item.toolName || '未指定'}</dd></div>
        <div><dt>来源</dt><dd>{item.agent || '当前运行'}</dd></div>
        <div><dt>目标</dt><dd title={permissionTarget(item.arguments)}>{permissionTarget(item.arguments)}</dd></div>
      </dl>
      {args ? (
        <details className="permission-arguments" open={pending && isHighRisk}>
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
          <Button
            data-testid="permission-deny"
            disabled={Boolean(resolving)}
            loading={resolving === 'deny'}
            onClick={() => handleResolve('deny')}
            variant="ghost"
          >
            拒绝
          </Button>
          <Button
            data-testid="permission-approve"
            disabled={Boolean(resolving)}
            loading={resolving === 'approve'}
            onClick={() => handleResolve('approve')}
            variant="default"
          >
            允许一次
          </Button>
        </div>
      )}
    </article>
  );
}
