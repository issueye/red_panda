import { selectEffectiveMessages } from './tokenBudget.js';

/** Helpers for selecting public Run content or one Worker's Assignment content. */

/**
 * Keep complete history in session storage, but do not keep rendering messages
 * that the active context summary has already replaced.
 */
export function filterVisibleMessagesAfterCompaction(messages = [], compaction = null) {
  return selectEffectiveMessages(messages, compaction);
}

/**
 * Root-facing rows for the main conversation tab.
 * Collaborative Worker trails (tools, message deltas, permissions) belong on
 * Worker tabs only — otherwise the main chat is polluted by intermediate work.
 *
 * Root Runtime always emits profile_key "root". Delegated workers use another
 * profile or leave it empty while still setting worker/assignment ids.
 *
 * @param {object} item
 * @returns {boolean}
 */
export function isMainConversationItem(item) {
  if (!item) return false;
  if (item.visibility === 'worker_private') return false;

  // Optimistic / UI-only rows (no Runtime worker attribution).
  if (item.role === 'user') return true;
  if (item.agent === 'system' || item.agent === 'gateway') return true;

  const profile = String(item.profileKey || '').trim().toLowerCase();
  if (profile === 'root') return true;

  // Explicit non-root profile → collaborative Worker content.
  if (profile) return false;

  // Empty profile with worker/assignment markers is a delegated Worker trail
  // (common for tool_calls projected without profile_key).
  if (item.assignmentId || item.workerId) return false;

  return true;
}

export function filterMainMessages(messages = []) {
  return messages.filter((item) => isMainConversationItem(item));
}

export function filterMainTools(tools = []) {
  return tools.filter((item) => isMainConversationItem(item));
}

export function filterMainPermissions(permissions = []) {
  return permissions.filter((item) => isMainConversationItem(item));
}

function matchesWorker(scope, item) {
  if (!scope || !item) return false;
  if (scope.assignmentId && item.assignmentId) return item.assignmentId === scope.assignmentId;
  return Boolean(scope.workerId && item.workerId === scope.workerId);
}

export function filterWorkerMessages(scope, messages = []) {
  return messages.filter((item) => matchesWorker(scope, item));
}
export function filterWorkerTools(scope, tools = []) {
  return tools.filter((item) => matchesWorker(scope, item));
}
export function filterWorkerPermissions(scope, permissions = []) {
  return permissions.filter((item) => matchesWorker(scope, item));
}

export function extractWorkerScope(event = {}) {
  return {
    assignmentId: event.assignment_id || '',
    workerId: event.worker?.id || '',
    profileKey: event.worker?.profile_key || '',
  };
}
