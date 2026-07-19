import { useCallback } from 'react';
import { projectPermissionDecision } from '../lib/permissionActions.js';

/**
 * Permission approve/deny actions for the shell (docs/47 Wave D2).
 */
export function usePermissionActions({
  permissions,
  globalPendingPermissions,
  currentRunId,
  setPermissions,
  setGlobalPendingPermissions,
  setRuns,
  request,
}) {
  const resolvePermission = useCallback(async (id, decision) => {
    // Snapshot lists for run_id resolution; list updates stay functional.
    const projected = projectPermissionDecision({
      permissions,
      globalPendingPermissions,
      runs: [],
      id,
      decision,
      currentRunId,
    });
    setPermissions((items) => projectPermissionDecision({
      permissions: items,
      globalPendingPermissions: [],
      runs: [],
      id,
      decision,
      currentRunId,
    }).permissions);
    setGlobalPendingPermissions((items) => items.filter((permission) => permission.id !== id));
    setRuns((items) => projectPermissionDecision({
      permissions,
      globalPendingPermissions,
      runs: items,
      id,
      decision,
      currentRunId,
    }).runs);
    await request('permission.resolve', {
      permission_id: id,
      run_id: projected.runId || currentRunId,
      decision,
    }).catch(() => {});
  }, [
    permissions,
    globalPendingPermissions,
    currentRunId,
    setPermissions,
    setGlobalPendingPermissions,
    setRuns,
    request,
  ]);

  return { resolvePermission };
}
