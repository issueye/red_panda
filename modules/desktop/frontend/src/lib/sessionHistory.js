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
