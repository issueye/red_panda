import { classNames } from '../../lib/format.js';

export function PanelHeader({ action, className, subtitle, title }) {
  return (
    <div className={classNames('panel-header', className)}>
      <div>
        <strong>{title}</strong>
        {subtitle ? <span>{subtitle}</span> : null}
      </div>
      {action || null}
    </div>
  );
}
