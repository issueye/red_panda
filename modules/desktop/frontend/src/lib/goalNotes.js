/**
 * Goal scratchpad notes (context.*) helpers for Desktop read-only projection.
 * Gateway remains authority; Desktop only hydrates and displays.
 */

export const GOAL_NOTE_KIND_LABELS = {
  finding: '发现',
  decision: '决策',
  risk: '风险',
  fact: '事实',
  handoff: '交接',
  note: '笔记',
};

/**
 * @param {any} raw
 */
export function normalizeGoalNote(raw = {}) {
  return {
    id: raw.id || '',
    goalId: raw.goal_id || raw.goalId || '',
    kind: String(raw.kind || 'note').toLowerCase(),
    title: raw.title || '',
    body: raw.body || '',
    phase: raw.phase || '',
    source: raw.source || '',
    runId: raw.run_id || raw.runId || '',
    seq: Number(raw.seq ?? 0),
    pinned: Boolean(raw.pinned === true || raw.pinned === 1 || raw.pinned === '1'),
    createdAt: raw.created_at || raw.createdAt || '',
    updatedAt: raw.updated_at || raw.updatedAt || '',
  };
}

/**
 * @param {any} data
 * @returns {ReturnType<typeof normalizeGoalNote>[]}
 */
export function normalizeGoalNoteList(data) {
  if (Array.isArray(data)) return data.map(normalizeGoalNote);
  if (Array.isArray(data?.items)) return data.items.map(normalizeGoalNote);
  if (Array.isArray(data?.notes)) return data.notes.map(normalizeGoalNote);
  return [];
}

export function goalNoteKindLabel(kind) {
  const key = String(kind || '').toLowerCase();
  return GOAL_NOTE_KIND_LABELS[key] || kind || '笔记';
}

/**
 * One-line summary for the strip list (title preferred, else truncated body).
 * @param {ReturnType<typeof normalizeGoalNote>} note
 * @param {number} [maxLen]
 */
export function goalNoteHeadline(note, maxLen = 80) {
  if (!note) return '';
  const title = String(note.title || '').trim();
  if (title) return title;
  const body = String(note.body || '').trim().replace(/\s+/g, ' ');
  if (!body) return note.id || '笔记';
  return body.length > maxLen ? `${body.slice(0, maxLen)}…` : body;
}
