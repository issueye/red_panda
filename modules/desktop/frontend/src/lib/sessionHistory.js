import { apiJson } from './api.js';

const HISTORY_PAGE_SIZE = 200;
const SESSION_PAGE_SIZE = 100;

export async function loadAllSessions(request = apiJson) {
  const items = [];
  let offset = 0;
  for (;;) {
    const page = await request(`/api/v1/sessions?offset=${offset}&limit=${SESSION_PAGE_SIZE}`);
    const pageItems = Array.isArray(page?.items) ? page.items : [];
    items.push(...pageItems);
    if (!page?.has_more) return items;
    const next = Number(page.next_offset);
    if (!Number.isInteger(next) || next <= offset) {
      throw new Error('session list cursor did not advance');
    }
    offset = next;
  }
}

/** Load complete visible history through the Gateway sequence cursor. */
export async function loadAllSessionHistory(sessionId, request = apiJson) {
  if (!sessionId) return [];
  const items = [];
  let afterSeq = 0;
  for (;;) {
    const page = await request(
      `/api/v1/sessions/${encodeURIComponent(sessionId)}/history?after_seq=${afterSeq}&limit=${HISTORY_PAGE_SIZE}`,
    );
    const pageItems = Array.isArray(page?.items) ? page.items : [];
    items.push(...pageItems);
    if (!page?.has_more) return items;
    const next = Number(page.next_after_seq) || 0;
    if (next <= afterSeq) {
      throw new Error('session history cursor did not advance');
    }
    afterSeq = next;
  }
}

/**
 * Single-round-trip session hydrate (docs/48 Wave D).
 * Falls back to multi-endpoint fetch when bootstrap is unavailable.
 *
 * @param {string} sessionId
 * @param {(path: string, opts?: object) => Promise<any>} [request]
 */
export async function loadSessionBootstrap(sessionId, request = apiJson) {
  if (!sessionId) {
    throw new Error('session id is required');
  }
  try {
    const data = await request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/bootstrap`);
    if (data && typeof data === 'object' && (data.history || data.runs || data.context)) {
      const historyItems = Array.isArray(data.history?.items)
        ? data.history.items
        : Array.isArray(data.history)
          ? data.history
          : [];
      return {
        history: historyItems,
        runs: Array.isArray(data.runs) ? data.runs : [],
        tools: Array.isArray(data.tools) ? data.tools : [],
        permissions: Array.isArray(data.permissions) ? data.permissions : [],
        todos: data.todos ?? null,
        context: data.context ?? null,
        goals: data.goals ?? null,
      };
    }
  } catch {
    // Older Gateway without bootstrap — fall through.
  }
  const [history, runs, tools, permissions, todos, context, goals] = await Promise.all([
    loadAllSessionHistory(sessionId, request),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/runs`).catch(() => []),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/tools`).catch(() => []),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/permissions`).catch(() => []),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/todos`).catch(() => null),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/context`).catch(() => null),
    request(`/api/v1/sessions/${encodeURIComponent(sessionId)}/goals`).catch(() => null),
  ]);
  return {
    history: Array.isArray(history) ? history : [],
    runs: Array.isArray(runs) ? runs : [],
    tools: Array.isArray(tools) ? tools : [],
    permissions: Array.isArray(permissions) ? permissions : [],
    todos,
    context,
    goals,
  };
}
