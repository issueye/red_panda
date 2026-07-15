import { forwardRef } from 'react';
import { classNames } from '../../lib/format.js';

export const Button = forwardRef(function Button({ className, children, icon, variant = 'default', ...props }, ref) {
  let childComp;
  if (children) {
    childComp = <span>{children}</span>;
  } else {
    childComp = null;
   }
  return (
    <button className={classNames('button', `button-${variant}`, className)} ref={ref} type="button" {...props}>
      {icon ? <span className="button-icon">{icon}</span> : null}
      {childComp}
    </button>
  );
});

export const IconButton = forwardRef(function IconButton({ label, className, children, variant = 'ghost', ...props }, ref) {
  return (
    <button
      aria-label={label}
      className={classNames('icon-button', `button-${variant}`, className)}
      ref={ref}
      title={label}
      type="button"
      {...props}
    >
      {children}
    </button>
  );
});
