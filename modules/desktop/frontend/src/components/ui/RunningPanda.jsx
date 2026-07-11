import { classNames } from '../../lib/format.js';

/**
 * Inline red-panda mascot with hop / blink / ear animations.
 * Drawn without the app-icon frame so it reads cleanly as a status actor.
 */
function PandaMascot() {
  return (
    <svg
      aria-hidden="true"
      className="running-panda-svg"
      fill="none"
      viewBox="0 0 48 48"
      xmlns="http://www.w3.org/2000/svg"
    >
      {/* soft ground shadow is CSS */}
      <g className="running-panda-body">
        {/* ears */}
        <g className="running-panda-ear running-panda-ear-left">
          <rect fill="#8A3412" height="8" rx="2" width="8" x="10" y="8" />
          <rect fill="#D95F24" height="4" width="4" x="12" y="10" />
        </g>
        <g className="running-panda-ear running-panda-ear-right">
          <rect fill="#8A3412" height="8" rx="2" width="8" x="30" y="8" />
          <rect fill="#D95F24" height="4" width="4" x="32" y="10" />
        </g>

        {/* head / face */}
        <rect fill="#D95F24" height="24" rx="6" width="24" x="12" y="12" />
        <rect fill="#F97316" height="8" width="16" x="16" y="10" />
        <rect fill="#FFF7ED" height="12" width="16" x="16" y="22" />

        {/* eye patches */}
        <rect fill="#7C2D12" height="8" width="8" x="14" y="18" />
        <rect fill="#7C2D12" height="8" width="8" x="26" y="18" />

        {/* eyes (blink via CSS scaleY) */}
        <g className="running-panda-eyes">
          <rect className="running-panda-eye" fill="#111827" height="4" width="4" x="16" y="20" />
          <rect className="running-panda-eye" fill="#111827" height="4" width="4" x="28" y="20" />
        </g>

        {/* nose + smile */}
        <rect fill="#111827" height="3" width="4" x="22" y="27" />
        <rect fill="#8A3412" height="2" width="10" x="19" y="32" />

        {/* cheeks */}
        <rect fill="#FB923C" height="3" opacity="0.55" width="4" x="13" y="28" />
        <rect fill="#FB923C" height="3" opacity="0.55" width="4" x="31" y="28" />

        {/* little paws */}
        <rect className="running-panda-paw" fill="#C2410C" height="5" rx="1.5" width="7" x="11" y="35" />
        <rect className="running-panda-paw" fill="#C2410C" height="5" rx="1.5" width="7" x="30" y="35" />
      </g>
    </svg>
  );
}

/**
 * Compact red-panda activity indicator shown while a run is in progress.
 */
export function RunningPanda({
  className = '',
  label = '小熊猫思考中',
  size = 'md',
  showLabel = true,
  variant = 'pill',
}) {
  return (
    <div
      aria-live="polite"
      className={classNames('running-panda', `size-${size}`, `variant-${variant}`, className)}
      data-testid="running-panda"
      role="status"
    >
      <span className="running-panda-figure" aria-hidden="true">
        <PandaMascot />
        <span className="running-panda-shadow" />
        <span className="running-panda-glow" />
      </span>

      <span className="running-panda-copy">
        {showLabel ? <span className="running-panda-label">{label}</span> : null}
        <span className="running-panda-dots" aria-hidden="true">
          <i />
          <i />
          <i />
        </span>
      </span>
    </div>
  );
}
