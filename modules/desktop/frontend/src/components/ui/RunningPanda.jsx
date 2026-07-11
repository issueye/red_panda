import mark from '../../assets/red-panda-mark.svg';
import { classNames } from '../../lib/format.js';

/**
 * Compact red-panda activity indicator shown while a run is in progress.
 */
export function RunningPanda({
  className = '',
  label = '小熊猫工作中',
  size = 'md',
  showLabel = true,
}) {
  return (
    <div
      aria-live="polite"
      className={classNames('running-panda', `size-${size}`, className)}
      data-testid="running-panda"
      role="status"
    >
      <span className="running-panda-figure" aria-hidden="true">
        <img alt="" className="running-panda-face" draggable={false} src={mark} />
        <span className="running-panda-shadow" />
      </span>
      <span className="running-panda-dots" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
      {showLabel ? <span className="running-panda-label">{label}</span> : null}
    </div>
  );
}
