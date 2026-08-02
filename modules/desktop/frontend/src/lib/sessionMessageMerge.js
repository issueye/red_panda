function messageText(message) {
  return String(message?.text ?? message?.content ?? '');
}

function isPersisted(message) {
  return (Number(message?.messageSeq) || 0) > 0;
}

function streamedMessageKey(message) {
  if (!message || !['assistant', 'reasoning'].includes(message.role) || message.agent === 'system') return '';
  const runId = String(message.runId || '');
  const assignmentId = String(message.assignmentId || '');
  const workerId = String(message.workerId || '');
  const profileKey = String(message.profileKey || '');
  if (!runId && !assignmentId && !workerId && !profileKey) return '';
  const visibility = message.visibility === 'worker_private' ? 'worker_private' : 'run_public';
  return [message.role, runId, assignmentId, workerId, profileKey, visibility].join('\u0000');
}

function aggregatePersistedStreamText(messages) {
  const result = new Map();
  for (const message of messages) {
    if (!isPersisted(message)) continue;
    const key = streamedMessageKey(message);
    if (!key) continue;
    result.set(key, `${result.get(key) || ''}${messageText(message)}`);
  }
  return result;
}

function persistedAdvance(previousText, historyText) {
  if (!previousText) return historyText;
  if (historyText.startsWith(previousText)) return historyText.slice(previousText.length);
  return historyText;
}

function reconcileStreamRows(rows, previousPersistedText, historyPersistedText) {
  const liveText = rows.map(messageText).join('');
  if (!liveText) return rows;

  const advance = persistedAdvance(previousPersistedText, historyPersistedText);
  let coveredLength = 0;
  if (advance && liveText.startsWith(advance)) {
    coveredLength = advance.length;
  } else if (advance && advance.includes(liveText)) {
    coveredLength = liveText.length;
  }

  if (coveredLength <= 0) return rows;
  const retained = [];
  let remaining = coveredLength;
  for (const row of rows) {
    const text = messageText(row);
    if (remaining >= text.length) {
      remaining -= text.length;
      continue;
    }
    retained.push(remaining > 0 ? { ...row, text: text.slice(remaining) } : row);
    remaining = 0;
  }
  return retained;
}

function optimisticUserMatches(previous, history) {
  const previousCounts = new Map();
  const historyCounts = new Map();
  for (const message of previous) {
    if (message?.role !== 'user' || !isPersisted(message)) continue;
    const text = messageText(message);
    previousCounts.set(text, (previousCounts.get(text) || 0) + 1);
  }
  for (const message of history) {
    if (message?.role !== 'user' || !isPersisted(message)) continue;
    const text = messageText(message);
    historyCounts.set(text, (historyCounts.get(text) || 0) + 1);
  }

  const matches = new Map();
  for (const [text, count] of historyCounts) {
    const added = count - (previousCounts.get(text) || 0);
    if (added > 0) matches.set(text, added);
  }
  return matches;
}

/**
 * Replace the UI transcript with authoritative persisted history while keeping
 * optimistic or streaming rows that have not reached the history endpoint yet.
 */
export function mergeSessionHistoryMessages(previousMessages = [], historyMessages = []) {
  const history = Array.isArray(historyMessages) ? historyMessages : [];
  const previous = Array.isArray(previousMessages) ? previousMessages : [];
  if (history.length === 0) return previous;

  const historyIds = new Set(history.map((message) => message?.id).filter(Boolean));
  const previousPersisted = aggregatePersistedStreamText(previous);
  const historyPersisted = aggregatePersistedStreamText(history);
  const streamRowsByKey = new Map();

  for (const message of previous) {
    if (!message || isPersisted(message) || (message.id && historyIds.has(message.id))) continue;
    const key = streamedMessageKey(message);
    if (!key) continue;
    const rows = streamRowsByKey.get(key) || [];
    rows.push(message);
    streamRowsByKey.set(key, rows);
  }

  const retainedStreamRows = new Set();
  const trimmedStreamRows = new Map();
  for (const [key, rows] of streamRowsByKey) {
    const retained = reconcileStreamRows(
      rows,
      previousPersisted.get(key) || '',
      historyPersisted.get(key) || '',
    );
    for (const row of retained) {
      const original = rows.find((candidate) => candidate === row || candidate.id === row.id);
      if (!original) continue;
      retainedStreamRows.add(original);
      if (row !== original) trimmedStreamRows.set(original, row);
    }
  }

  const userMatches = optimisticUserMatches(previous, history);
  const extras = [];
  for (const message of previous) {
    if (!message || isPersisted(message) || (message.id && historyIds.has(message.id))) continue;
    if (message.agent === 'system') {
      extras.push(message);
      continue;
    }
    if (message.role === 'user' && String(message.id || '').startsWith('user_')) {
      const text = messageText(message);
      const remaining = userMatches.get(text) || 0;
      if (remaining > 0) {
        userMatches.set(text, remaining - 1);
      } else {
        extras.push(message);
      }
      continue;
    }
    const key = streamedMessageKey(message);
    if (key) {
      if (retainedStreamRows.has(message)) {
        extras.push(trimmedStreamRows.get(message) || message);
      }
      continue;
    }
    extras.push(message);
  }

  return extras.length > 0 ? [...history, ...extras] : history;
}

/**
 * Replace persisted output rows with the run-event projection for each
 * represented run/role. User messages and legacy runs without events remain.
 */
export function replaceHistoryWithRunStreams(historyMessages = [], streamMessages = []) {
  const history = Array.isArray(historyMessages) ? historyMessages : [];
  const streams = Array.isArray(streamMessages) ? streamMessages : [];
  if (streams.length === 0) return history;

  const aggregate = (messages) => {
    const result = new Map();
    for (const message of messages) {
      const key = streamedMessageKey(message);
      if (!key) continue;
      result.set(key, `${result.get(key) || ''}${messageText(message)}`);
    }
    return result;
  };
  const historyText = aggregate(history);
  const streamText = aggregate(streams);
  const replaceable = new Set();
  for (const [key, text] of streamText) {
    const persisted = historyText.get(key) || '';
    if (!persisted || persisted === text) replaceable.add(key);
  }

  const retained = history.filter((message) => !replaceable.has(streamedMessageKey(message)));
  const acceptedStreams = streams.filter((message) => replaceable.has(streamedMessageKey(message)));
  return [...retained, ...acceptedStreams];
}
