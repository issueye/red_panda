/** Lifecycle statuses for subagents — never take tool call statuses (e.g. tool completed). */
export const SUBAGENT_LIFECYCLE_STATUSES = new Set([
  'queued',
  'starting',
  'running',
  'completed',
  'failed',
  'cancelled',
  'cancelling',
  'reset',
  'idle',
]);

/**
 * Resolve subagent lifecycle status for the right panel / conversation tabs.
 * Only `subagent_update` may set terminal/lifecycle status. Tool events often
 * carry payload.status=completed for the *tool*, which must not complete the agent.
 *
 * @param {string} eventType
 * @param {Record<string, unknown> | undefined} payloadBody
 * @param {string | undefined} currentStatus
 * @returns {string}
 */
export function resolveSubAgentLifecycleStatus(eventType, payloadBody, currentStatus) {
  if (eventType === 'subagent_update') {
    const next = String(payloadBody?.status || '').trim();
    if (next && SUBAGENT_LIFECYCLE_STATUSES.has(next)) {
      return next;
    }
    return currentStatus || 'running';
  }

  // Live activity from a subagent means it is still working.
  if (
    eventType === 'tool_started'
    || eventType === 'tool_output'
    || eventType === 'tool_finished'
    || eventType === 'tool_failed'
    || eventType === 'message_delta'
    || eventType === 'reasoning_delta'
    || eventType === 'permission_required'
  ) {
    if (!currentStatus || currentStatus === 'idle' || currentStatus === 'reset') {
      return 'running';
    }
    // Do not demote terminal states on late tool echoes after a true completion.
    if (currentStatus === 'completed' || currentStatus === 'failed' || currentStatus === 'cancelled') {
      return currentStatus;
    }
    return 'running';
  }

  return currentStatus || 'running';
}
