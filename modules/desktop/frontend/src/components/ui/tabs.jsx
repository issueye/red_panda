import { classNames } from '../../lib/format.js';

export function TabButton({ active, children, className, ...props }) {
  return (
    <button
      className={classNames('right-tab', active && 'active', className)}
      type="button"
      {...props}
    >
      {children}
    </button>
  );
}
