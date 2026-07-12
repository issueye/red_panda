/**
 * In-memory diagnostic log for Desktop (settings viewer + optional console).
 * LLM request bodies are recorded by Agent Runtime when logLlmRequests is on;
 * this module covers UI/gateway client-side breadcrumbs.
 */

const MAX_ENTRIES = 400;

/** @type {Array<{ id: string, ts: string, level: string, source: string, message: string, detail?: string }>} */
let entries = [];
/** @type {Set<(items: typeof entries) => void>} */
const listeners = new Set();

function emit() {
  const snapshot = entries.slice();
  for (const listener of listeners) {
    try {
      listener(snapshot);
    } catch {
      // Listeners must not break logging.
    }
  }
}

/**
 * @param {'debug' | 'info' | 'warn' | 'error'} level
 * @param {string} message
 * @param {{ source?: string, detail?: unknown }} [meta]
 */
export function appendDiagnosticLog(level, message, meta = {}) {
  const detail = meta.detail === undefined || meta.detail === null
    ? ''
    : typeof meta.detail === 'string'
      ? meta.detail
      : (() => {
        try {
          return JSON.stringify(meta.detail, null, 2);
        } catch {
          return String(meta.detail);
        }
      })();
  const entry = {
    id: `log_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`,
    ts: new Date().toISOString(),
    level: level || 'info',
    source: meta.source || 'desktop',
    message: String(message || ''),
    detail,
  };
  entries = [...entries.slice(-(MAX_ENTRIES - 1)), entry];
  if (typeof console !== 'undefined') {
    const line = `[red-panda:${entry.source}] ${entry.message}`;
    if (level === 'error' && console.error) console.error(line, meta.detail || '');
    else if (level === 'warn' && console.warn) console.warn(line, meta.detail || '');
    else if (console.debug) console.debug(line, meta.detail || '');
  }
  emit();
  return entry;
}

export function getDiagnosticLogs() {
  return entries.slice();
}

export function clearDiagnosticLogs() {
  entries = [];
  emit();
}

/**
 * @param {(items: typeof entries) => void} listener
 * @returns {() => void}
 */
export function subscribeDiagnosticLogs(listener) {
  if (typeof listener !== 'function') return () => {};
  listeners.add(listener);
  listener(entries.slice());
  return () => {
    listeners.delete(listener);
  };
}

/** Default Agent LLM log directory hint (matches runtime defaultLLMLogDir). */
export function defaultLlmLogDirHint() {
  if (typeof navigator !== 'undefined' && /Win/i.test(navigator.platform || navigator.userAgent || '')) {
    return '%AppData%\\red-panda\\logs\\llm-requests';
  }
  return '~/.config/red-panda/logs/llm-requests';
}
