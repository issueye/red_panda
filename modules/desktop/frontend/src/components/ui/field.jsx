import { classNames } from '../../lib/format.js';
import { HelpTooltip } from './tooltip.jsx';

/**
 * Form field with optional label help tooltip.
 * Structure avoids nesting interactive help inside a <label> control target.
 */
export function Field({ children, className, label, tooltip }) {
  return (
    <div className={classNames('ui-field', className)}>
      {label ? (
        <div className="ui-field-label-row">
          <span className="ui-field-label">{label}</span>
          {tooltip ? <HelpTooltip content={tooltip} /> : null}
        </div>
      ) : null}
      {children}
    </div>
  );
}
