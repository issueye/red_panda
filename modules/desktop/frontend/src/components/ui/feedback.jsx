import { classNames } from '../../lib/format.js';

export function EmptyState({ children, className, title, ...props }) {
  return (
    <div className={classNames('ui-empty-state', className)} {...props}>
      {title ? <strong>{title}</strong> : null}
      {children ? <span>{children}</span> : null}
    </div>
  );
}

export function InlineEmpty({ children, className, as: Component = 'p', ...props }) {
  return (
    <Component className={classNames('ui-inline-empty', className)} {...props}>
      {children}
    </Component>
  );
}

export function ErrorMessage({ children, className, as: Component = 'p', ...props }) {
  if (!children) {
    return null;
  }
  return (
    <Component className={classNames('ui-error-message', className)} {...props}>
      {children}
    </Component>
  );
}
