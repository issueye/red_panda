/**
 * Normalize Worker assignment projections from Gateway / WS payloads.
 */
export function normalizeAssignment(item = {}) {
  return {
    id: item.id || '',
    runId: item.run_id || item.runId || '',
    workerId: item.worker_id || item.workerId || '',
    originWorkerId: item.origin_worker_id || item.originWorkerId || '',
    profileKey: item.profile_key || item.profileKey || '',
    task: item.task || '',
    attempt: Number(item.attempt) || 1,
    retryOf: item.retry_of || item.retryOf || '',
    retrying: Boolean(item.retrying),
    retryInMs: Number(item.retry_in_ms ?? item.retryInMs) || 0,
    status: item.status || 'queued',
    result: item.result || '',
    error: item.error || '',
    summary: item.summary || '',
    createdAt: item.created_at || item.createdAt,
    startedAt: item.started_at || item.startedAt,
    finishedAt: item.finished_at || item.finishedAt,
    workerSeq: Number(item.worker_seq ?? item.workerSeq) || 0,
  };
}
