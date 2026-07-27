import { classNames } from '../../lib/format.js';

/**
 * @param {{
 *   active?: boolean,
 *   badge?: number | string | null,
 *   badgeTone?: 'default' | 'warning' | 'danger' | 'info',
 *   children: import('react').ReactNode,
 *   className?: string,
 *   panelId?: string,
 * }} props
 */
export function TabButton({
  active,
  badge = null,
  badgeTone = 'default',
  children,
  className,
  panelId,
  ...props
}) {
  const badgeValue = badge == null || badge === '' ? null : badge;
  const showBadge = badgeValue != null && Number(badgeValue) !== 0;

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
      <span className="right-tab-label">{children}</span>
      {showBadge ? (
        <span
          className={classNames('right-tab-badge', `is-${badgeTone}`)}
          data-testid="right-tab-badge"
        >
          {Number(badgeValue) > 99 ? '99+' : badgeValue}
        </span>
      ) : null}
    </button>
  );
}
