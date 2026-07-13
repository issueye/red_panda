/** Run terminal events are distinct from Assignment lifecycle updates in v0.2. */
export function isRunTerminalEvent(event) {
  return event?.protocol_version === '2026-07-13'
    && Boolean(event?.run_id)
    && (event.type === 'finish' || event.type === 'error');
}
