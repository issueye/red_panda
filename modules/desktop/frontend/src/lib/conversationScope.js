/** Helpers for selecting public Run content or one Worker's Assignment content. */

export function filterMainMessages(messages = []) {
  return messages.filter((item) => item.visibility !== 'worker_private');
}

export function filterMainTools(tools = []) { return [...tools]; }
export function filterMainPermissions(permissions = []) { return [...permissions]; }

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
