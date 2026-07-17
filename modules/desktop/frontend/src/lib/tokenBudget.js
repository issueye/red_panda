/**
 * Rough context-token estimation for Desktop budget ring + auto-compact.
 * Not a true tokenizer; good enough for progress UX.
 */

/** When estimated context usage reaches this fraction of max tokens, auto-compact. */
export const CONTEXT_AUTO_COMPACT_RATIO = 0.8;
/** Soft ceiling when provider max_tokens is unset — keeps the ring visually responsive. */
export const SOFT_CONTEXT_BUDGET = 32_000;
/** Minimum visible arc when any tokens are used (SVG progress readability). */
export const MIN_VISIBLE_RATIO = 0.03;

/**
 * @param {string} text
 * @returns {number}
 */
export function estimateTextTokens(text = '') {
  const value = String(text || '');
  if (!value) return 0;
  let tokens = 0;
  for (const ch of value) {
    const code = ch.codePointAt(0) || 0;
    // CJK / fullwidth / kana etc. ~1 token; latin/punctuation ~4 chars/token.
    if (code > 0x2e80) {
      tokens += 1;
    } else {
      tokens += 0.25;
    }
  }
  return Math.max(0, Math.ceil(tokens));
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function messageBodyText(value) {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  if (Array.isArray(value)) {
    return value.map((part) => {
      if (typeof part === 'string') return part;
      if (part && typeof part === 'object' && typeof part.text === 'string') return part.text;
      return '';
    }).join('\n');
  }
  if (typeof value === 'object' && typeof value.text === 'string') return value.text;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

/**
 * @param {Array<{ text?: string, content?: string, role?: string }>} messages
 * @param {string} [draft]
 * @param {Array<{ output?: string, error?: string, name?: string }>} [tools]
 * @returns {number}
 */
export function estimateSessionTokens(messages = [], draft = '', tools = []) {
  let total = 0;
  for (const message of messages) {
    const body = messageBodyText(message?.text ?? message?.content ?? '');
    total += estimateTextTokens(body);
    // Role / framing overhead.
    total += 6;
  }
  for (const tool of tools) {
    total += estimateTextTokens(tool?.output || '');
    total += estimateTextTokens(tool?.error || '');
    total += estimateTextTokens(tool?.name || '');
    total += 8;
  }
  total += estimateTextTokens(draft);
  return total;
}

/**
 * Index where the "kept tail" starts when preserving the last N user-led turns.
 * Matches gateway planCompactionByTurns for live UI transcripts without messageSeq.
 * @param {Array<{ role?: string }>} messages
 * @param {number} turns
 * @returns {number}
 */
export function coveredCountForKeepTailTurns(messages = [], turns = 0) {
  const n = Math.max(0, Math.floor(Number(turns) || 0));
  if (n <= 0 || !Array.isArray(messages) || messages.length === 0) {
    return Array.isArray(messages) ? messages.length : 0;
  }
  const userIdx = [];
  for (let i = 0; i < messages.length; i += 1) {
    if (messages[i]?.role === 'user') userIdx.push(i);
  }
  if (userIdx.length === 0) {
    const keep = Math.min(messages.length, n * 2);
    return Math.max(0, messages.length - keep);
  }
  if (userIdx.length <= n) return 0;
  return userIdx[userIdx.length - n];
}

/**
 * Messages that still count toward model-facing context after an in-place summary.
 * Live / optimistic messages often lack messageSeq; they must not be dropped when
 * endSeq > 0 or the budget ring freezes at 0 after the first compact.
 * @param {Array<{ messageSeq?: number, role?: string }>} messages
 * @param {{ endSeq?: number, coveredCount?: number, keepTailTurns?: number } | null} compaction
 */
export function selectEffectiveMessages(messages = [], compaction = null) {
  const list = Array.isArray(messages) ? messages : [];
  const endSeq = Math.max(0, Number(compaction?.endSeq) || 0);
  if (endSeq <= 0) return list;

  const coveredCount = Math.max(0, Number(compaction?.coveredCount) || 0);
  const hasSeqs = list.some((message) => (Number(message?.messageSeq) || 0) > 0);

  if (hasSeqs) {
    return list.filter((message, index) => {
      const seq = Number(message?.messageSeq) || 0;
      if (seq > 0) return seq > endSeq;
      // Unsequenced live rows (streaming / optimistic user bubbles).
      // When a coveredCount boundary exists, only count messages at/after it.
      if (coveredCount > 0) return index >= coveredCount;
      return true;
    });
  }

  if (coveredCount > 0) {
    return list.slice(Math.min(coveredCount, list.length));
  }

  const keepTailTurns = Math.max(0, Number(compaction?.keepTailTurns) || 0);
  if (keepTailTurns > 0) {
    const start = coveredCountForKeepTailTurns(list, keepTailTurns);
    return list.slice(start);
  }

  // No boundary available — keep full list rather than reporting 0 forever.
  return list;
}

/**
 * Estimate model-facing context while preserving the complete UI history.
 * Messages covered by the active summary are replaced only for this estimate.
 * @param {Array<{ messageSeq?: number, text?: string, content?: string }>} messages
 * @param {string} draft
 * @param {Array<object>} tools
 * @param {{ endSeq?: number, summary?: unknown, coveredCount?: number, keepTailTurns?: number } | null} compaction
 */
export function estimateEffectiveSessionTokens(messages = [], draft = '', tools = [], compaction = null) {
  const endSeq = Math.max(0, Number(compaction?.endSeq) || 0);
  if (endSeq <= 0) return estimateSessionTokens(messages, draft, tools);
  const tail = selectEffectiveMessages(messages, compaction);
  let summaryText = '';
  try {
    summaryText = JSON.stringify(compaction?.summary || '');
  } catch {
    summaryText = String(compaction?.summary || '');
  }
  // Historical tool cards are UI audit data and are not re-sent in a new run.
  return estimateSessionTokens(tail, draft, []) + estimateTextTokens(summaryText) + 8;
}

/**
 * Map used/max into a ring fill ratio. Applies a tiny floor so low usage still
 * draws a visible arc (otherwise 200/128000 looks "broken").
 * @param {number} used
 * @param {number} maxTokens
 * @returns {number}
 */
export function ringFillRatio(used, maxTokens) {
  const max = Number(maxTokens) || 0;
  const safeUsed = Math.max(0, Number(used) || 0);
  if (max <= 0) return 0;
  if (safeUsed <= 0) return 0;
  const raw = Math.min(1, safeUsed / max);
  return Math.max(MIN_VISIBLE_RATIO, raw);
}

/**
 * @param {number} used
 * @param {number} maxTokens
 * @returns {{
 *   used: number,
 *   maxTokens: number,
 *   ratio: number,
 *   displayRatio: number,
 *   enabled: boolean,
 *   softBudget: boolean,
 *   autoCompact: boolean,
 * }}
 */
export function tokenBudgetState(used, maxTokens) {
  const max = Number(maxTokens) || 0;
  const safeUsed = Math.max(0, Number(used) || 0);
  if (max <= 0) {
    // Unset budget: still animate against a soft ceiling so the ring is not dead.
    const softMax = Math.max(SOFT_CONTEXT_BUDGET, safeUsed || SOFT_CONTEXT_BUDGET);
    const ratio = ringFillRatio(safeUsed, softMax);
    return {
      used: safeUsed,
      maxTokens: 0,
      ratio,
      displayRatio: ratio,
      enabled: false,
      softBudget: true,
      autoCompact: false,
    };
  }
  const ratio = Math.min(1, safeUsed / max);
  return {
    used: safeUsed,
    maxTokens: max,
    ratio,
    displayRatio: ringFillRatio(safeUsed, max),
    enabled: true,
    softBudget: false,
    autoCompact: ratio >= CONTEXT_AUTO_COMPACT_RATIO,
  };
}

/**
 * @param {number} value
 * @returns {string}
 */
export function formatTokenCount(value) {
  const n = Math.max(0, Math.round(Number(value) || 0));
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 10_000) return `${Math.round(n / 1000)}k`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}
