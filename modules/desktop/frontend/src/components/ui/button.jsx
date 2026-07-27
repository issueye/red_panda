import { forwardRef } from 'react';
import { Loader2 } from 'lucide-react';
import { classNames } from '../../lib/format.js';

export const Button = forwardRef(function Button({
  className,
  children,
  icon,
  variant = 'default',
  loading = false,
  disabled = false,
  type = 'button',
  ...props
}, ref) {
  const isDisabled = disabled || loading;
  let childComp = null;
  if (children) {
    childComp = <span>{children}</span>;
  }

  return (
    <button
      className={classNames('button', `button-${variant}`, loading && 'is-loading', className)}
      disabled={isDisabled}
      ref={ref}
      type={type}
      {...props}
      aria-busy={loading || undefined}
    >
      {loading ? (
        <span className="button-icon button-spinner" aria-hidden="true">
          <Loader2 className="button-spin" size={15} />
        </span>
      ) : icon ? (
        <span className="button-icon">{icon}</span>
      ) : null}
      {childComp}
    </button>
  );
});

export const IconButton = forwardRef(function IconButton({
  label,
  className,
  children,
  variant = 'ghost',
  loading = false,
  disabled = false,
  type = 'button',
  ...props
}, ref) {
  const isDisabled = disabled || loading;
  return (
    <button
      aria-busy={loading || undefined}
      aria-label={label}
      className={classNames('icon-button', `button-${variant}`, loading && 'is-loading', className)}
      disabled={isDisabled}
      ref={ref}
      title={label}
      type={type}
      {...props}
    >
      {loading ? <Loader2 aria-hidden="true" className="button-spin" size={15} /> : children}
    </button>
  );
});
