/**
 * Permission resolve optimistic projection (docs/47 Wave D2).
 */

/**
 * @param {object} params
 * @param {Array} params.permissions
 * @param {Array} params.globalPendingPermissions
 * @param {Array} params.runs
 * @param {string} params.id
 * @param {string} params.decision
 * @param {string} [params.currentRunId]
 * @returns {{
 *   item: object|undefined,
 *   permissions: Array,
 *   globalPendingPermissions: Array,
 *   runs: Array,
 *   runId: string,
 * }}
 */
export function projectPermissionDecision({
  permissions = [],
  globalPendingPermissions = [],
  runs = [],
  id,
  decision,
  currentRunId = '',
}) {
  const item = permissions.find((permission) => permission.id === id)
    || globalPendingPermissions.find((permission) => permission.id === id);
  const runId = item?.runId || currentRunId || '';
  return {
    item,
    runId,
    permissions: permissions.map((permission) => (
      permission.id === id
        ? { ...permission, status: 'resolved', decision }
        : permission
    )),
    globalPendingPermissions: globalPendingPermissions.filter((permission) => permission.id !== id),
    runs: runs.map((run) => (
      run.id === runId && run.status === 'waiting_permission'
        ? { ...run, status: 'running', updatedAt: new Date().toISOString() }
        : run
    )),
  };
}
