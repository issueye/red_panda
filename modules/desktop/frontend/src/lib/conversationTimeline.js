function positiveNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : 0;
}

function timestamp(value) {
  const time = Date.parse(value || '');
  return Number.isFinite(time) ? time : 0;
}

function timelineItem(type, value, index) {
  const isMessage = type === 'message';
  const runId = value.runId;
  const eventSeq = positiveNumber(value.runSeq);
  const createdAt = isMessage
    ? value.createdAt
    : value.startedAt || value.createdAt;
  return {
    type,
    value,
    key: `${type}:${value.id || index}`,
    runId: runId || '',
    eventSeq,
    timestamp: timestamp(createdAt),
    index,
    unsequencedUser: isMessage && value.role === 'user' && !eventSeq && !createdAt,
  };
}

/**
 * Stable chronological order for tool-call counters.
 * Prefer startedSeq / runSeq, then startedAt, then id.
 * @param {Record<string, unknown>} left
 * @param {Record<string, unknown>} right
 */
export function compareToolCallOrder(left, right) {
  const leftSeq = positiveNumber(left?.startedSeq || left?.runSeq);
  const rightSeq = positiveNumber(right?.startedSeq || right?.runSeq);
  if (leftSeq && rightSeq && leftSeq !== rightSeq) return leftSeq - rightSeq;
  if (leftSeq !== rightSeq) return leftSeq ? -1 : 1;

  const leftTime = timestamp(left?.startedAt || left?.createdAt);
  const rightTime = timestamp(right?.startedAt || right?.createdAt);
  if (leftTime && rightTime && leftTime !== rightTime) return leftTime - rightTime;
  if (leftTime !== rightTime) return leftTime ? -1 : 1;

  return String(left?.id || '').localeCompare(String(right?.id || ''));
}

/**
 * Map tool id → 1-based call index for display (e.g. 1/12).
 * @param {Array<Record<string, unknown>>} tools
 * @returns {Map<string, number>}
 */
export function buildToolCallIndexMap(tools = []) {
  const sorted = [...tools].sort(compareToolCallOrder);
  const map = new Map();
  sorted.forEach((tool, index) => {
    if (tool?.id == null || tool.id === '') return;
    map.set(String(tool.id), index + 1);
  });
  return map;
}

export function buildConversationTimeline(messages = [], tools = [], permissions = []) {
  const items = [];
  messages.forEach((item) => items.push(timelineItem('message', item, items.length)));
  tools.forEach((item) => items.push(timelineItem('tool', item, items.length)));
  permissions.forEach((item) => items.push(timelineItem('permission', item, items.length)));

  return items.sort((left, right) => {
    if (left.runId && left.runId === right.runId && left.eventSeq && right.eventSeq) {
      const sequenceOrder = left.eventSeq - right.eventSeq;
      if (sequenceOrder !== 0) return sequenceOrder;
    }

    if (left.timestamp && right.timestamp) {
      const timeOrder = left.timestamp - right.timestamp;
      if (timeOrder !== 0) return timeOrder;
    }

    if (left.unsequencedUser !== right.unsequencedUser) {
      return left.unsequencedUser ? -1 : 1;
    }

    if (left.eventSeq && right.eventSeq) {
      const sequenceOrder = left.eventSeq - right.eventSeq;
      if (sequenceOrder !== 0) return sequenceOrder;
    }
    if (left.eventSeq !== right.eventSeq) {
      return left.eventSeq ? -1 : 1;
    }
    if (left.timestamp !== right.timestamp) {
      return left.timestamp ? -1 : 1;
    }
    return left.index - right.index;
  });
}
