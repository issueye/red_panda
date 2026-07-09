import { Check, ChevronDown } from 'lucide-react';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
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

export function SelectMenu({
  ariaLabel,
  className,
  disabled = false,
  id,
  onChange,
  options,
  testId,
  value,
}) {
  const generatedId = useId();
  const listboxId = `${id || generatedId}-listbox`;
  const rootRef = useRef(null);
  const buttonRef = useRef(null);
  const itemRefs = useRef([]);
  const normalizedOptions = useMemo(() => normalizeOptions(options), [options]);
  const selectedIndex = Math.max(0, normalizedOptions.findIndex((option) => option.value === value));
  const selected = normalizedOptions.find((option) => option.value === value) || normalizedOptions[0];
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(selectedIndex);

  useEffect(() => {
    if (!open) {
      return undefined;
    }
    const onPointerDown = (event) => {
      if (rootRef.current && !rootRef.current.contains(event.target)) {
        setOpen(false);
      }
    };
    const onKeyDown = (event) => {
      if (event.key === 'Escape') {
        setOpen(false);
        buttonRef.current?.focus();
      }
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  // 自定义下拉不用原生 select，这里手动维护选项焦点，确保键盘操作不会丢失焦点位置。
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

  return (
    <div className={classNames('select-menu', className)} ref={rootRef}>
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
      {open ? (
        <div className="select-listbox" id={listboxId} role="listbox">
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
        </div>
      ) : null}
    </div>
  );
}
