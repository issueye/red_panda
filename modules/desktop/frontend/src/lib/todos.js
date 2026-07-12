/**
 * Session todo list helpers (operational checklist, not memory).
 */

const OPEN_STATUSES = new Set(['pending', 'in_progress']);

/**
 * @param {any} raw
 */
export function normalizeTodo(raw = {}) {
  const status = String(raw.status || 'pending').toLowerCase();
  return {
    id: raw.id || '',
    clientKey: raw.client_key || raw.clientKey || '',
    content: raw.content || '',
    status,
    sortOrder: Number(raw.sort_order ?? raw.sortOrder ?? 0),
    priority: raw.priority || 'medium',
    activeForm: raw.active_form || raw.activeForm || '',
    sessionId: raw.session_id || raw.sessionId || '',
    updatedAt: raw.updated_at || raw.updatedAt || '',
  };
}

/**
 * @param {Array} items
 */
export function countOpenTodos(items = []) {
  return items.filter((item) => OPEN_STATUSES.has(item.status)).length;
}

/**
 * @param {any} payload tool_finished payload
 * @returns {{ items: ReturnType<typeof normalizeTodo>[], openCount: number } | null}
 */
export function todosFromToolFinishedPayload(payload) {
  const name = payload?.tool_name || payload?.name || '';
  if (name !== 'todo.write' && name !== 'todo.list' && name !== 'todo_write') {
    return null;
  }
  const envelope = parseToolResultV1(payload?.output);
  if (!envelope?.ok) return null;
  const itemsRaw = envelope?.data?.items;
  if (!Array.isArray(itemsRaw)) return null;
  const items = itemsRaw.map(normalizeTodo);
  return {
    items,
    openCount: Number(envelope.data.open_count ?? countOpenTodos(items)),
  };
}

/**
 * @param {any} body todo_updated event body
 */
export function todosFromUpdatedEvent(body = {}) {
  const items = Array.isArray(body.items) ? body.items.map(normalizeTodo) : [];
  return {
    items,
    openCount: Number(body.open_count ?? countOpenTodos(items)),
  };
}

/**
 * @param {string|object} output
 */
export function parseToolResultV1(output) {
  if (!output) return null;
  let value = output;
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== 'object') return null;
  if (value.schema !== 'red_panda.tool_result.v1') {
    // Still accept if it looks like our envelope.
    if (value.ok === undefined && !value.data) return null;
  }
  return value;
}

export function todoStatusLabel(status) {
  switch (String(status || '').toLowerCase()) {
    case 'pending':
      return '待办';
    case 'in_progress':
      return '进行中';
    case 'completed':
      return '已完成';
    case 'cancelled':
      return '已取消';
    default:
      return status || '未知';
  }
}
