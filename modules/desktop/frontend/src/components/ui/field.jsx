import { classNames } from '../../lib/format.js';

export function Field({ children, className, label }) {
  return (
    <label className={classNames('ui-field', className)}>
      <span>{label}</span>
      {children}
    </label>
  );
}
