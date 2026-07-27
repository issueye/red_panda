import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { AlertTriangle, Check, Info, X, XCircle } from 'lucide-react';
import { classNames } from '../../lib/format.js';
import { IconButton } from './button.jsx';

const ToastContext = createContext(null);

const DEFAULT_DURATION = {
  success: 2400,
  info: 3200,
  warning: 4200,
  error: 0, // sticky until dismissed
};

let toastSeq = 0;

function nextToastId() {
  toastSeq += 1;
  return `toast_${Date.now()}_${toastSeq}`;
}

/**
 * @typedef {'success' | 'info' | 'warning' | 'error'} ToastTone
 * @typedef {{
 *   id?: string,
 *   title?: string,
 *   message: string,
 *   tone?: ToastTone,
 *   durationMs?: number,
 * }} ToastInput
 */

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    throw new Error('useToast must be used within ToastProvider');
  }
  return ctx;
}

/** Safe for optional use outside provider (tests / story fixtures). */
export function useOptionalToast() {
  return useContext(ToastContext);
}

export function ToastProvider({ children, maxVisible = 4 }) {
  const [toasts, setToasts] = useState([]);
  const timersRef = useRef(new Map());

  const dismiss = useCallback((id) => {
    const timer = timersRef.current.get(id);
    if (timer) {
      window.clearTimeout(timer);
      timersRef.current.delete(id);
    }
    setToasts((current) => current.filter((item) => item.id !== id));
  }, []);

  const clear = useCallback(() => {
    timersRef.current.forEach((timer) => window.clearTimeout(timer));
    timersRef.current.clear();
    setToasts([]);
  }, []);

  const push = useCallback((input) => {
    const tone = input.tone || 'info';
    const id = input.id || nextToastId();
    const durationMs = input.durationMs ?? DEFAULT_DURATION[tone] ?? DEFAULT_DURATION.info;
    const toast = {
      id,
      title: input.title || '',
      message: String(input.message || '').trim(),
      tone,
      durationMs,
      createdAt: Date.now(),
    };
    if (!toast.message) return id;

    setToasts((current) => {
      const withoutDup = current.filter((item) => item.id !== id);
      const next = [...withoutDup, toast];
      return next.slice(Math.max(0, next.length - maxVisible));
    });

    if (durationMs > 0) {
      const existing = timersRef.current.get(id);
      if (existing) window.clearTimeout(existing);
      timersRef.current.set(
        id,
        window.setTimeout(() => dismiss(id), durationMs),
      );
    }

    return id;
  }, [dismiss, maxVisible]);

  const api = useMemo(() => ({
    push,
    dismiss,
    clear,
    success: (message, options = {}) => push({ ...options, message, tone: 'success' }),
    info: (message, options = {}) => push({ ...options, message, tone: 'info' }),
    warning: (message, options = {}) => push({ ...options, message, tone: 'warning' }),
    error: (message, options = {}) => push({ ...options, message, tone: 'error' }),
  }), [clear, dismiss, push]);

  useEffect(() => () => {
    timersRef.current.forEach((timer) => window.clearTimeout(timer));
    timersRef.current.clear();
  }, []);

  return (
    <ToastContext.Provider value={api}>
      {children}
      <ToastViewport toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

function toneIcon(tone) {
  if (tone === 'success') return <Check size={14} strokeWidth={2.5} />;
  if (tone === 'warning') return <AlertTriangle size={14} />;
  if (tone === 'error') return <XCircle size={14} />;
  return <Info size={14} />;
}

function ToastViewport({ toasts, onDismiss }) {
  if (!toasts.length) return null;

  return (
    <div
      aria-live="polite"
      className="toast-viewport"
      data-testid="toast-viewport"
    >
      {toasts.map((toast) => (
        <div
          className={classNames('toast', `toast-${toast.tone}`)}
          data-testid="toast"
          data-tone={toast.tone}
          key={toast.id}
          role={toast.tone === 'error' || toast.tone === 'warning' ? 'alert' : 'status'}
        >
          <span aria-hidden="true" className="toast-icon">
            {toneIcon(toast.tone)}
          </span>
          <div className="toast-body">
            {toast.title ? <strong className="toast-title">{toast.title}</strong> : null}
            <span className="toast-message">{toast.message}</span>
          </div>
          <IconButton
            className="toast-dismiss"
            data-testid="toast-dismiss"
            label="关闭通知"
            onClick={() => onDismiss(toast.id)}
            variant="ghost"
          >
            <X size={14} />
          </IconButton>
        </div>
      ))}
    </div>
  );
}
