import { classNames } from '../../lib/format.js';

export function Button({ className, children, icon, variant = 'default', ...props }) {
  return (
    <button className={classNames('button', `button-${variant}`, className)} type="button" {...props}>
      {icon ? <span className="button-icon">{icon}</span> : null}
      <span>{children}</span>
    </button>
  );
}

export function IconButton({ label, className, children, variant = 'ghost', ...props }) {
  return (
    <button
      aria-label={label}
      className={classNames('icon-button', `button-${variant}`, className)}
      title={label}
      type="button"
      {...props}
    >
      {children}
    </button>
  );
}
