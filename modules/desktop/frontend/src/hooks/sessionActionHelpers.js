import { normalizeRun } from '../lib/sessionNormalize.js';

function timestamp(now) {
  return new Date(now()).toISOString();
}

export function createUserMessage(text, now = Date.now) {
  return {
    id: `user_${now()}`,
    role: 'user',
    createdAt: timestamp(now),
    text,
  };
}

export function createSystemMessage(idPrefix, text, { includeCreatedAt = false, now = Date.now } = {}) {
  return {
    id: `${idPrefix}_${now()}`,
    role: 'assistant',
    agent: 'system',
    ...(includeCreatedAt ? { createdAt: timestamp(now) } : {}),
    text,
  };
}

export function appendMessages(runtime, ...messages) {
  return {
    ...runtime,
    messages: [...runtime.messages, ...messages],
  };
}

export function beginRunProjection(runtime, displayText) {
  return {
    ...appendMessages(runtime, createUserMessage(displayText)),
    draft: '',
    running: true,
    runSeq: 0,
    assignmentsById: {},
    assignmentOrder: [],
  };
}

export function beginGoalContinuationProjection(runtime, displayText) {
  return {
    ...appendMessages(runtime, createUserMessage(displayText)),
    draft: '',
    running: true,
    runSeq: 0,
  };
}

export function applyStartedRunProjection(runtime, {
  displayText,
  result,
  runSettings,
  sessionId,
  workspace,
}) {
  const nextRunId = result?.run_id || '';
  return {
    ...runtime,
    running: true,
    currentRunId: nextRunId || runtime.currentRunId,
    runSeq: nextRunId ? (Number(runtime.runSeqByRun?.[nextRunId]) || 0) : runtime.runSeq,
    runs: nextRunId
      ? [
          normalizeRun({
            id: nextRunId,
            session_id: sessionId,
            workspace_root: workspace?.root_path || workspace?.root || '',
            runtime_mode: result?.runtime_mode || runSettings.runtimeMode,
            status: 'running',
            input: displayText,
            last_run_seq: result?.run_seq || runtime.runSeq,
            message_count: 1,
            tool_count: 0,
            started_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          }),
          ...runtime.runs.filter((item) => item.id !== nextRunId),
        ]
      : runtime.runs,
  };
}

export function normalizeWorkspaceRoot(root) {
  return (root || '').replace(/\\/g, '/').toLowerCase();
}
