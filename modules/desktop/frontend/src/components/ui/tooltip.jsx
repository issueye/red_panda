import { CircleHelp } from 'lucide-react';
import {
  cloneElement,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';
import { classNames } from '../../lib/format.js';

const DEFAULT_DELAY = 180;
const VIEWPORT_PAD = 8;

/**
 * Lightweight tooltip. Anchors to a single child; opens on hover/focus.
 *
 * @param {object} props
 * @param {import('react').ReactNode} props.content
 * @param {import('react').ReactElement} props.children
 * @param {'top'|'bottom'|'left'|'right'} [props.side]
 * @param {number} [props.delay]
 * @param {string} [props.className]
 * @param {boolean} [props.disabled]
 */
export function Tooltip({
  content,
  children,
  side = 'top',
  delay = DEFAULT_DELAY,
  className,
  disabled = false,
}) {
  const tooltipId = useId();
  const triggerRef = useRef(null);
  const tipRef = useRef(null);
  const timerRef = useRef(null);
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0, placement: side });

  const clearTimer = useCallback(() => {
    if (timerRef.current != null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const show = useCallback(() => {
    if (disabled || content == null || content === '') return;
    clearTimer();
    timerRef.current = window.setTimeout(() => setOpen(true), delay);
  }, [clearTimer, content, delay, disabled]);

  const hide = useCallback(() => {
    clearTimer();
    setOpen(false);
  }, [clearTimer]);

  useEffect(() => () => clearTimer(), [clearTimer]);

  useLayoutEffect(() => {
    if (!open || !triggerRef.current || !tipRef.current) return undefined;

    function place() {
      const trigger = triggerRef.current?.getBoundingClientRect();
      const tip = tipRef.current?.getBoundingClientRect();
      if (!trigger || !tip) return;

      const vw = window.innerWidth;
      const vh = window.innerHeight;
      let placement = side;
      let top = 0;
      let left = 0;

      const prefer = {
        top: trigger.top - tip.height - 8,
        bottom: trigger.bottom + 8,
        left: trigger.left - tip.width - 8,
        right: trigger.right + 8,
      };

      if (placement === 'top' && prefer.top < VIEWPORT_PAD) placement = 'bottom';
      if (placement === 'bottom' && prefer.bottom + tip.height > vh - VIEWPORT_PAD) placement = 'top';
      if (placement === 'left' && prefer.left < VIEWPORT_PAD) placement = 'right';
      if (placement === 'right' && prefer.right + tip.width > vw - VIEWPORT_PAD) placement = 'left';

      if (placement === 'top' || placement === 'bottom') {
        top = placement === 'top' ? prefer.top : prefer.bottom;
        left = trigger.left + trigger.width / 2 - tip.width / 2;
      } else {
        left = placement === 'left' ? prefer.left : prefer.right;
        top = trigger.top + trigger.height / 2 - tip.height / 2;
      }

      left = Math.min(Math.max(VIEWPORT_PAD, left), vw - tip.width - VIEWPORT_PAD);
      top = Math.min(Math.max(VIEWPORT_PAD, top), vh - tip.height - VIEWPORT_PAD);

      setCoords({ top, left, placement });
    }

    place();
    window.addEventListener('scroll', place, true);
    window.addEventListener('resize', place);
    return () => {
      window.removeEventListener('scroll', place, true);
      window.removeEventListener('resize', place);
    };
  }, [open, side, content]);

  if (!isValidElement(children)) {
    return children;
  }

  const child = cloneElement(children, {
    ref: (node) => {
      triggerRef.current = node;
      const { ref } = children;
      if (typeof ref === 'function') ref(node);
      else if (ref && typeof ref === 'object') ref.current = node;
    },
    onMouseEnter: (event) => {
      children.props.onMouseEnter?.(event);
      show();
    },
    onMouseLeave: (event) => {
      children.props.onMouseLeave?.(event);
      hide();
    },
    onFocus: (event) => {
      children.props.onFocus?.(event);
      show();
    },
    onBlur: (event) => {
      children.props.onBlur?.(event);
      hide();
    },
    'aria-describedby': open ? tooltipId : children.props['aria-describedby'],
  });

  return (
    <>
      {child}
      {open && typeof document !== 'undefined'
        ? createPortal(
          <div
            className={classNames(
              'ui-tooltip',
              `ui-tooltip-${coords.placement || side}`,
              className,
            )}
            id={tooltipId}
            ref={tipRef}
            role="tooltip"
            style={{ top: coords.top, left: coords.left }}
          >
            <div className="ui-tooltip-content">{content}</div>
          </div>,
          document.body,
        )
        : null}
    </>
  );
}

/**
 * Compact “?” help control for form labels and dense settings rows.
 */
export function HelpTooltip({
  content,
  label = '说明',
  side = 'top',
  className,
  testId,
}) {
  if (content == null || content === '') return null;
  return (
    <Tooltip content={content} side={side}>
      <button
        aria-label={label}
        className={classNames('ui-help-tooltip-trigger', className)}
        data-testid={testId}
        type="button"
      >
        <CircleHelp aria-hidden size={13} strokeWidth={2} />
      </button>
    </Tooltip>
  );
}
