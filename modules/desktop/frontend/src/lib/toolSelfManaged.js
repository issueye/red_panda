/**
 * Tools that manage their own timeouts / are expected to run for a long time.
 * The UI should not mark these as "stale" or warn about "network tool timeout".
 *
 * Keep this list in sync with backend:
 *   modules/agent/internal/tools/runner.go → toolTimeoutFor()
 */
const SELF_MANAGED_TOOL_NAMES = new Set([
  'shell.exec',
  'web.search',
  'web.fetch',
  'skill.run',
	'worker.delegate',
	'worker.cancel',
	'worker.send',
  'worker.receive',
]);

/**
 * Returns true if the given tool name is expected to run long and manages its own lifetime.
 * @param {string | undefined | null} name
 */
export function isSelfManagedTool(name) {
  if (!name) return false;
  const normalized = String(name).trim().replace(/__/g, '.');
  return SELF_MANAGED_TOOL_NAMES.has(normalized);
}

/**
 * Returns true if a running tool with this name should be treated as "stale"
 * after a long elapsed time (i.e. it is NOT self-managed).
 * @param {string | undefined | null} name
 * @param {number} elapsedMs
 * @param {number} [thresholdMs=25000]
 */
export function shouldMarkToolStale(name, elapsedMs, thresholdMs = 25000) {
  if (!Number.isFinite(elapsedMs) || elapsedMs < thresholdMs) return false;
  return !isSelfManagedTool(name);
}
