import { X } from 'lucide-react';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from 'react';
import { classNames } from '../../lib/format.js';
import { Button, IconButton } from './button.jsx';

const DialogContext = createContext(null);

/**
 * @typedef {{
 *   title?: string,
 *   message?: string,
 *   confirmLabel?: string,
 *   cancelLabel?: string,
 *   tone?: 'default' | 'danger',
 *   description?: string,
 * }} ConfirmOptions
 */

/**
 * @typedef {{
 *   title?: string,
 *   message?: string,
 *   confirmLabel?: string,
 *   defaultValue?: string,
 *   placeholder?: string,
 *   label?: string,
 * }} PromptOptions
 */

export function useDialog() {
  const ctx = useContext(DialogContext);
  if (!ctx) {
    throw new Error('useDialog must be used within DialogProvider');
  }
  return ctx;
}

/**
 * Optional dialog helper for components that may render outside provider during tests.
 * Falls back to browser dialogs only when provider is missing.
 */
export function useOptionalDialog() {
  return useContext(DialogContext);
}

export function DialogProvider({ children }) {
  const [active, setActive] = useState(null);
  const resolveRef = useRef(null);

  const close = useCallback((result) => {
    const resolve = resolveRef.current;
    resolveRef.current = null;
    setActive(null);
    resolve?.(result);
  }, []);

  const confirm = useCallback((options = {}) => new Promise((resolve) => {
    resolveRef.current = resolve;
    setActive({
      kind: 'confirm',
      title: options.title || '确认操作',
      message: options.message || '',
      description: options.description || '',
      confirmLabel: options.confirmLabel || '确定',
      cancelLabel: options.cancelLabel || '取消',
      tone: options.tone || 'default',
      testId: options.testId || 'dialog-confirm',
    });
  }), []);

  const alert = useCallback((options = {}) => new Promise((resolve) => {
    resolveRef.current = resolve;
    setActive({
      kind: 'alert',
      title: options.title || '提示',
      message: options.message || '',
      description: options.description || '',
      confirmLabel: options.confirmLabel || '知道了',
      tone: options.tone || 'default',
      testId: options.testId || 'dialog-alert',
    });
  }), []);

  const prompt = useCallback((options = {}) => new Promise((resolve) => {
    resolveRef.current = resolve;
    setActive({
      kind: 'prompt',
      title: options.title || '请输入',
      message: options.message || '',
      description: options.description || '',
      label: options.label || '',
      placeholder: options.placeholder || '',
      defaultValue: options.defaultValue || '',
      confirmLabel: options.confirmLabel || '确定',
      cancelLabel: options.cancelLabel || '取消',
      tone: options.tone || 'default',
      testId: options.testId || 'dialog-prompt',
    });
  }), []);

  const openCustom = useCallback((options = {}) => new Promise((resolve) => {
    resolveRef.current = resolve;
    setActive({
      kind: 'custom',
      ...options,
      testId: options.testId || 'dialog-custom',
    });
  }), []);

  const value = useMemo(() => ({ confirm, alert, prompt, openCustom, close }), [confirm, alert, prompt, openCustom, close]);

  return (
    <DialogContext.Provider value={value}>
      {children}
      {active ? (
        <DialogHost state={active} onClose={close} />
      ) : null}
    </DialogContext.Provider>
  );
}

/**
 * Presentational dialog shell used by provider host and custom modals.
 */
export function Dialog({
  open = true,
  title,
  description,
  children,
  footer,
  onClose,
  size = 'md',
  testId = 'dialog',
  className,
}) {
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    const previous = document.activeElement;
    const frame = window.requestAnimationFrame(() => {
      const focusable = panelRef.current?.querySelector(
        'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      focusable?.focus();
    });
    return () => {
      window.cancelAnimationFrame(frame);
      if (previous && typeof previous.focus === 'function') {
        previous.focus();
      }
    };
  }, [open]);

  useEffect(() => {
    if (!open) return undefined;
    function onKeyDown(event) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose?.();
      }
    }
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="ui-dialog-overlay" data-testid={`${testId}-overlay`} role="presentation">
      <button
        aria-label="关闭对话框"
        className="ui-dialog-backdrop"
        onClick={() => onClose?.()}
        type="button"
      />
      <div
        aria-describedby={description ? descriptionId : undefined}
        aria-labelledby={titleId}
        aria-modal="true"
        className={classNames('ui-dialog', `ui-dialog-${size}`, className)}
        data-testid={testId}
        ref={panelRef}
        role="dialog"
      >
        <header className="ui-dialog-header">
          <div className="ui-dialog-heading">
            {title ? <strong id={titleId}>{title}</strong> : <span id={titleId} className="ui-dialog-sr-only">对话框</span>}
            {description ? <span id={descriptionId}>{description}</span> : null}
          </div>
          {onClose ? (
            <IconButton label="关闭" onClick={onClose}>
              <X size={16} />
            </IconButton>
          ) : null}
        </header>
        {children ? <div className="ui-dialog-body">{children}</div> : null}
        {footer ? <footer className="ui-dialog-footer">{footer}</footer> : null}
      </div>
    </div>
  );
}

function DialogHost({ state, onClose }) {
  const [promptValue, setPromptValue] = useState(state.defaultValue || '');
  const inputRef = useRef(null);

  useEffect(() => {
    setPromptValue(state.defaultValue || '');
  }, [state]);

  useEffect(() => {
    if (state.kind === 'prompt') {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [state]);

  if (state.kind === 'custom' && typeof state.render === 'function') {
    return state.render({ close: onClose, state });
  }

  const isDanger = state.tone === 'danger';
  const footer = (
    <>
      {state.kind !== 'alert' ? (
        <Button onClick={() => onClose(state.kind === 'prompt' ? null : false)} variant="ghost">
          {state.cancelLabel || '取消'}
        </Button>
      ) : null}
      <Button
        className={isDanger ? 'button-danger' : undefined}
        data-testid={`${state.testId}-ok`}
        onClick={() => {
          if (state.kind === 'prompt') onClose(promptValue);
          else if (state.kind === 'confirm') onClose(true);
          else onClose(true);
        }}
        variant="default"
      >
        {state.confirmLabel || '确定'}
      </Button>
    </>
  );

  return (
    <Dialog
      description={state.description}
      footer={footer}
      onClose={() => onClose(state.kind === 'prompt' ? null : false)}
      testId={state.testId}
      title={state.title}
    >
      {state.message ? <p className="ui-dialog-message">{state.message}</p> : null}
      {state.kind === 'prompt' ? (
        <label className="ui-dialog-field">
          {state.label ? <span>{state.label}</span> : null}
          <input
            data-testid={`${state.testId}-input`}
            onChange={(event) => setPromptValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault();
                onClose(promptValue);
              }
            }}
            placeholder={state.placeholder}
            ref={inputRef}
            value={promptValue}
          />
        </label>
      ) : null}
    </Dialog>
  );
}
