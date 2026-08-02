import { Check, ChevronRight, Loader2, ShieldAlert, SquareTerminal, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { classNames } from '../lib/format.js';
import { ToolCallCard } from './ToolCallCard.jsx';

function groupState(items) {
  const statuses = items.map((item) => item.status || 'running');
  const failed = statuses.filter((status) => status === 'failed' || status === 'denied').length;
  const attention = statuses.filter((status) => status === 'waiting_permission').length;
  const running = statuses.filter((status) => status === 'running' || status === 'pending').length;
  if (attention) return { status: 'attention', action: '等待授权', label: `${attention} 个等待授权`, icon: <ShieldAlert size={13} /> };
  if (running) return { status: 'running', action: '正在运行', label: `${running} 个进行中`, icon: <Loader2 className="tool-spin" size={13} /> };
  if (failed) return { status: 'failed', action: '运行失败', label: `${failed} 个失败`, icon: <X size={13} /> };
  return { status: 'completed', action: '已运行', label: '已完成', icon: <Check size={13} strokeWidth={2.5} /> };
}

export function ToolExecutionGroup({ items = [] }) {
  const [open, setOpen] = useState(false);
  const state = useMemo(() => groupState(items), [items]);
  const toolSummary = useMemo(() => {
    const names = items
      .map((item) => item.displayName || item.name || '工具')
      .filter((name, index, values) => values.indexOf(name) === index);
    const visible = names.slice(0, 3);
    const remaining = Math.max(0, names.length - visible.length);
    return `${visible.join(' · ')}${remaining ? ` · +${remaining}` : ''}`;
  }, [items]);
  if (items.length === 0) return null;

  return (
    <section
      aria-busy={state.status === 'running' ? 'true' : undefined}
      className={classNames('tool-execution-group', open && 'is-expanded', `is-${state.status}`)}
      data-testid="tool-execution-group"
      data-timeline-type="tool"
    >
      <button
        aria-label={`${state.action} ${toolSummary}，${items.length} 个工具，${state.label}`}
        aria-expanded={open}
        className="tool-execution-group-toggle"
        data-testid="tool-execution-group-toggle"
        onClick={() => setOpen((current) => !current)}
        type="button"
      >
        <span className="tool-execution-group-icon" aria-hidden="true">
          <SquareTerminal size={14} />
        </span>
        <span className="tool-execution-group-copy">
          <span className="tool-execution-group-heading">
            <strong>{state.action}</strong>
            <span className="tool-execution-group-summary" title={toolSummary}>{toolSummary}</span>
            <em>{items.length} 项</em>
          </span>
        </span>
        <span aria-hidden="true" className={classNames('tool-execution-group-status', `is-${state.status}`)}>
          {state.icon}
          {state.status === 'completed' ? null : state.label}
        </span>
        <ChevronRight
          aria-hidden="true"
          className={classNames('tool-execution-group-chevron', open && 'is-open')}
          size={14}
        />
      </button>
      {open ? (
        <div className="tool-execution-group-body" data-testid="tool-execution-group-body">
          {items.map((item, index) => (
            <ToolCallCard item={item} key={item.id || index} />
          ))}
        </div>
      ) : null}
    </section>
  );
}
