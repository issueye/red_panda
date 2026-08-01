/**
 * Normalize Worker assignment projections from Gateway / WS payloads.
 */
export function normalizeAssignment(item = {}) {
  const stats = item.execution_stats || item.executionStats || {};
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
    executionStats: {
      maxTurns: Number(stats.max_turns ?? stats.maxTurns) || 0,
      loopTurns: Number(stats.loop_turns ?? stats.loopTurns) || 0,
      providerRequests: Number(stats.provider_requests ?? stats.providerRequests) || 0,
      toolCallsRequested: Number(stats.tool_calls_requested ?? stats.toolCallsRequested) || 0,
      toolCallsExecuted: Number(stats.tool_calls_executed ?? stats.toolCallsExecuted) || 0,
      toolCallBudget: Number(stats.tool_call_budget ?? stats.toolCallBudget) || 0,
      maxTurnsReached: Boolean(stats.max_turns_reached ?? stats.maxTurnsReached),
      toolBudgetReached: Boolean(stats.tool_budget_reached ?? stats.toolBudgetReached),
    },
  };
}
