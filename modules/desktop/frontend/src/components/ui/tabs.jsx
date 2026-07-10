import { classNames } from '../../lib/format.js';

export function TabButton({ active, children, className, panelId, ...props }) {
  return (
    <button
      aria-controls={panelId}
      aria-selected={active}
      className={classNames('right-tab', active && 'active', className)}
      role="tab"
      tabIndex={active ? 0 : -1}
      type="button"
      {...props}
    >
      {children}
    </button>
  );
}
