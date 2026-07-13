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
