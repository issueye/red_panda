/**
 * Worker assignment action helpers (docs/47 Wave D2).
 */

const CANCELLABLE_STATUSES = new Set(['queued', 'running', 'waiting_permission']);

export function isCancellableAssignment(assignment) {
  return Boolean(
    assignment?.id
    && assignment.runId
    && CANCELLABLE_STATUSES.has(assignment.status),
  );
}

/**
 * Optimistic cancel projection for a single assignment in runtime state.
 * @param {object} runtime
 * @param {object} assignment
 * @param {{ status?: string, summary?: string }} patch
 */
export function patchAssignmentInRuntime(runtime, assignment, patch) {
  if (!assignment?.id) return runtime;
  return {
    ...runtime,
    assignmentsById: {
      ...runtime.assignmentsById,
      [assignment.id]: { ...assignment, ...patch },
    },
  };
}
