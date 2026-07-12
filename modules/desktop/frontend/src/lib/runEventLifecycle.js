/**
 * Only root-agent finish/error events terminate the root run. Subagent errors
 * are recoverable tool results that the root agent can summarize or retry.
 * @param {any} event
 */
export function isRootTerminalRunEvent(event) {
  if (event?.type !== 'finish' && event?.type !== 'error') return false;
  const agent = event?.agent || {};
  const subagentId = agent.subagent_id || event?.payload?.subagent_id;
  return agent.role !== 'subagent' && !subagentId;
}
