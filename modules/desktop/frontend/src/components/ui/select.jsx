import { Check, ChevronDown } from 'lucide-react';
import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { classNames } from '../../lib/format.js';

function normalizeOptions(options) {
  return options.map((option) => {
    if (Array.isArray(option)) {
      return { value: option[0], label: option[1] };
    }
    if (typeof option === 'string') {
      return { value: option, label: option };
    }
    return {
      value: option.value,
      label: option.label ?? option.value,
      disabled: option.disabled,
    };
  });
}

/**
 * Compact custom select. Supports portal positioning to avoid overflow clipping.
 * @param {{ placement?: 'auto' | 'top' | 'bottom' }} props
 */
export function SelectMenu({
  ariaLabel,
  className,
  disabled = false,
  id,
  onChange,
  options,
  placement = 'auto',
  testId,
  value,
}) {
  const generatedId = useId();
  const listboxId = `${id || generatedId}-listbox`;
  const rootRef = useRef(null);
  const buttonRef = useRef(null);
  const listboxRef = useRef(null);
  const itemRefs = useRef([]);
  const normalizedOptions = useMemo(() => normalizeOptions(options), [options]);
  const selectedIndex = Math.max(0, normalizedOptions.findIndex((option) => option.value === value));
  const selected = normalizedOptions.find((option) => option.value === value) || normalizedOptions[0];
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(selectedIndex);
  const [coords, setCoords] = useState(null);
  const [resolvedPlacement, setResolvedPlacement] = useState(placement === 'top' ? 'top' : 'bottom');

  function updatePosition() {
    const trigger = buttonRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const viewportPadding = 8;
    const maxHeight = 220;
    const spaceBelow = window.innerHeight - rect.bottom - viewportPadding;
    const spaceAbove = rect.top - viewportPadding;
    let nextPlacement = placement;
    if (placement === 'auto') {
      nextPlacement = spaceBelow < Math.min(maxHeight, 160) && spaceAbove > spaceBelow ? 'top' : 'bottom';
    }
    const available = nextPlacement === 'top' ? spaceAbove : spaceBelow;
    const height = Math.max(120, Math.min(maxHeight, available));
    const width = Math.max(rect.width, 160);
    let left = rect.left;
    if (left + width > window.innerWidth - viewportPadding) {
      left = Math.max(viewportPadding, window.innerWidth - width - viewportPadding);
    }
    setResolvedPlacement(nextPlacement);
    setCoords({
      left,
      width,
      maxHeight: height,
      top: nextPlacement === 'top' ? undefined : rect.bottom + 4,
      bottom: nextPlacement === 'top' ? window.innerHeight - rect.top + 4 : undefined,
    });
  }

  useLayoutEffect(() => {
    if (!open) {
      setCoords(null);
      return undefined;
    }
    updatePosition();
    const onReposition = () => updatePosition();
    window.addEventListener('resize', onReposition);
    window.addEventListener('scroll', onReposition, true);
    return () => {
      window.removeEventListener('resize', onReposition);
      window.removeEventListener('scroll', onReposition, true);
    };
  }, [open, placement, normalizedOptions.length]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    const onPointerDown = (event) => {
      const target = event.target;
      if (rootRef.current?.contains(target) || listboxRef.current?.contains(target)) {
        return;
      }
      setOpen(false);
    };
    const onKeyDown = (event) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        event.stopImmediatePropagation?.();
        setOpen(false);
        buttonRef.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    // Capture so Escape closes the list before parent dialogs handle it.
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [open]);

  useEffect(() => {
    if (open) {
      itemRefs.current[activeIndex]?.focus();
    }
  }, [activeIndex, open]);

  function choose(option) {
    if (!option || option.disabled) {
      return;
    }
    onChange?.(option.value);
    setOpen(false);
    buttonRef.current?.focus();
  }

  function move(delta) {
    if (normalizedOptions.length === 0) {
      return;
    }
    let next = activeIndex;
    for (let index = 0; index < normalizedOptions.length; index += 1) {
      next = (next + delta + normalizedOptions.length) % normalizedOptions.length;
      if (!normalizedOptions[next]?.disabled) {
        setActiveIndex(next);
        return;
      }
    }
  }

  function onTriggerKeyDown(event) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      setOpen(true);
      setActiveIndex(selectedIndex);
    }
  }

  function onOptionKeyDown(event, option) {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      move(1);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      move(-1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      choose(option);
    } else if (event.key === 'Tab') {
      setOpen(false);
    }
  }

  const listbox = open && coords ? createPortal(
    <div
      className={classNames('select-listbox', 'select-listbox-portal', `placement-${resolvedPlacement}`)}
      id={listboxId}
      ref={listboxRef}
      role="listbox"
      style={{
        position: 'fixed',
        left: `${coords.left}px`,
        width: `${coords.width}px`,
        maxHeight: `${coords.maxHeight}px`,
        top: coords.top != null ? `${coords.top}px` : 'auto',
        bottom: coords.bottom != null ? `${coords.bottom}px` : 'auto',
        zIndex: 200,
      }}
    >
      {normalizedOptions.map((option, index) => (
        <button
          aria-selected={option.value === value}
          className={classNames('select-option', option.value === value && 'active')}
          disabled={option.disabled}
          key={option.value}
          onClick={() => choose(option)}
          onKeyDown={(event) => onOptionKeyDown(event, option)}
          ref={(node) => {
            itemRefs.current[index] = node;
          }}
          role="option"
          tabIndex={index === activeIndex ? 0 : -1}
          type="button"
        >
          <span>{option.label}</span>
          {option.value === value ? <Check aria-hidden="true" size={13} /> : null}
        </button>
      ))}
    </div>,
    document.body,
  ) : null;

  return (
    <div className={classNames('select-menu', open && 'is-open', className)} ref={rootRef}>
      <button
        aria-controls={open ? listboxId : undefined}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-label={ariaLabel}
        className="select-trigger"
        data-testid={testId}
        disabled={disabled}
        onClick={() => {
          setOpen((current) => !current);
          setActiveIndex(selectedIndex);
        }}
        onKeyDown={onTriggerKeyDown}
        ref={buttonRef}
        type="button"
      >
        <span>{selected?.label || '请选择'}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </button>
      {listbox}
    </div>
  );
}
