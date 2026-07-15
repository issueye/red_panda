import { goalFromUpdatedEvent, pickFocusGoal } from './goals.js';
import { createEmptySessionRuntime } from './sessionRuntime.js';
import { todosFromToolFinishedPayload, todosFromUpdatedEvent } from './todos.js';

export const WORKER_PROTOCOL_VERSION = '2026-07-13';
const TERMINAL_ASSIGNMENT_STATUSES = new Set(['completed', 'failed', 'cancelled']);
const TERMINAL_RUN_STATUSES = new Set(['completed', 'failed', 'cancelled']);
const ACTIVE_ASSIGNMENT_STATUSES = new Set(['queued', 'running', 'cancelling', 'waiting_permission']);

function assignmentStatusForTerminalRun(runStatus) {
  if (runStatus === 'cancelled') return 'cancelled';
  if (runStatus === 'failed') return 'failed';
  return 'completed';
}

export function reconcileAssignmentsWithRuns(assignments = [], runs = []) {
  const runsById = new Map((runs || []).map((run) => [run.id, run]));
  return (assignments || []).map((assignment) => {
    if (!ACTIVE_ASSIGNMENT_STATUSES.has(assignment?.status)) return assignment;
    const run = runsById.get(assignment.runId);
    if (!run || !TERMINAL_RUN_STATUSES.has(run.status)) return assignment;
    const status = assignmentStatusForTerminalRun(run.status);
    return {
      ...assignment,
      status,
      summary: assignment.summary || (status === 'completed' ? '随运行完成' : '随运行结束'),
      finishedAt: assignment.finishedAt || run.finishedAt,
    };
  });
}

export function appendWorkerText(items, event, text) {
  const previous = items[items.length - 1];
  const canAppend = event.type === 'message_delta'
    && previous?.role === 'assistant'
    && previous.runId === event.run_id
    && previous.assignmentId === event.assignment_id
    && previous.workerId === event.worker?.id
    && previous.eventSeq > 0
    && Number(event.run_seq) === previous.eventSeq + 1;
  if (canAppend) {
    return [...items.slice(0, -1), {
      ...previous,
      eventSeq: Number(event.run_seq),
      runSeq: Number(event.run_seq),
      text: `${previous.text}${text}`,
    }];
  }
  return [...items, {
    id: event.event_id || `evt_${Date.now()}`,
    role: 'assistant',
    runId: event.run_id || '',
    assignmentId: event.assignment_id || '',
    workerId: event.worker?.id || '',
    profileKey: event.worker?.profile_key || '',
    eventSeq: Number(event.run_seq) || 0,
    runSeq: Number(event.run_seq) || 0,
    visibility: event.payload?.visibility || 'run_public',
    createdAt: event.created_at || new Date().toISOString(),
    text,
  }];
}

export const appendAgentText = appendWorkerText;

export function countToolsForRun(items, runId) {
  if (!Array.isArray(items) || !runId) return 0;
  return items.filter((item) => item.runId === runId).length;
}

export function updateRunByID(items, runId, patch) {
  let found = false;
  const next = items.map((item) => {
    if (item.id !== runId) return item;
    found = true;
    return { ...item, ...patch };
  });
  return found ? next : [...items, { id: runId, ...patch }];
}

export function upsertByID(items, nextItem) {
  return [...items.filter((item) => item.id !== nextItem.id), nextItem];
}

function updateTodos(next, body) {
  const parsed = todosFromUpdatedEvent(body);
  const shouldAutoExpand = parsed.openCount > 0
    && (next.todoOpenCount || 0) === 0
    && !next.todosAutoExpandedOnce;
  return {
    ...next,
    todos: parsed.items,
    todoOpenCount: parsed.openCount,
    todosVersion: (next.todosVersion || 0) + 1,
    todosHydrated: true,
    todosExpanded: shouldAutoExpand || next.todosExpanded,
    todosAutoExpandedOnce: shouldAutoExpand || next.todosAutoExpandedOnce,
  };
}

function assignmentFromEvent(event, current) {
  const body = event.payload?.assignment || event.payload || {};
  const status = body.status || current?.status || 'running';
  if (current && TERMINAL_ASSIGNMENT_STATUSES.has(current.status)
    && !TERMINAL_ASSIGNMENT_STATUSES.has(status)) {
    return current;
  }
  return {
    id: body.id || event.assignment_id,
    runId: body.run_id || event.run_id,
    workerId: body.worker_id || event.worker?.id || current?.workerId || '',
    originWorkerId: body.origin_worker_id || current?.originWorkerId || '',
    profileKey: body.profile_key || event.worker?.profile_key || current?.profileKey || '',
    task: body.task || current?.task || '',
    attempt: Number(body.attempt) || current?.attempt || 1,
    retryOf: body.retry_of || current?.retryOf || '',
    retrying: Boolean(body.retrying),
    retryInMs: Number(body.retry_in_ms) || current?.retryInMs || 0,
    status,
    result: body.result || current?.result || '',
    error: body.error || current?.error || '',
    summary: body.summary || event.payload?.summary || current?.summary || '',
    createdAt: body.created_at || current?.createdAt || event.created_at,
    startedAt: body.started_at || current?.startedAt,
    finishedAt: body.finished_at || current?.finishedAt,
    workerSeq: Number(event.worker_seq) || current?.workerSeq || 0,
  };
}

function upsertAssignment(runtime, assignment) {
  const exists = Boolean(runtime.assignmentsById?.[assignment.id]);
  return {
    ...runtime,
    assignmentsById: { ...(runtime.assignmentsById || {}), [assignment.id]: assignment },
    assignmentOrder: exists
      ? (runtime.assignmentOrder || [])
      : [...(runtime.assignmentOrder || []), assignment.id],
  };
}

export function reduceRunEvent(runtime, event) {
  const prev = runtime || createEmptySessionRuntime();
  const effects = [];
  if (event?.protocol_version !== WORKER_PROTOCOL_VERSION
    || !event.run_id || !event.session_id || !event.assignment_id || !event.worker?.id
    || !Number.isFinite(Number(event.run_seq)) || Number(event.run_seq) <= 0
    || !Number.isFinite(Number(event.worker_seq)) || Number(event.worker_seq) <= 0) {
    return { runtime: prev, effects: [{ type: 'ignore_incompatible_event', event }] };
  }
  const runId = event.run_id;
  const runSeq = Number(event.run_seq) || 0;
  const currentRun = prev.runs.find((item) => item.id === runId);
  if (currentRun && TERMINAL_RUN_STATUSES.has(currentRun.status)) {
    return { runtime: prev, effects };
  }
  const previousRunSeq = Number(prev.runSeqByRun?.[runId] ?? currentRun?.lastRunSeq ?? 0) || 0;
  if (runSeq > 0 && runSeq <= previousRunSeq && event.event_id) {
    return { runtime: prev, effects };
  }
  let next = {
    ...prev,
    runSeq,
    runSeqByRun: { ...(prev.runSeqByRun || {}), [runId]: runSeq },
    running: prev.running || event.type !== 'finish',
    currentRunId: runId,
    hydrated: true,
  };

  if (event.type === 'worker_assignment_updated') {
    const current = next.assignmentsById?.[event.assignment_id];
    const assignment = assignmentFromEvent(event, current);
    next = upsertAssignment(next, assignment);
    effects.push({ type: 'upsert_worker_assignment', assignment, worker: event.worker });
    return { runtime: next, effects };
  }

  if (event.type === 'permission_required') {
    const permission = {
      id: event.payload?.permission_id || `perm_${Date.now()}`,
      runId,
      sessionId: event.session_id,
      assignmentId: event.assignment_id,
      workerId: event.worker?.id || '',
      profileKey: event.worker?.profile_key || '',
      status: 'pending',
      summary: event.payload?.summary || '需要授权',
      detail: event.payload?.detail || event.payload?.tool_name || '系统正在等待处理决定。',
      risk: event.payload?.risk,
      toolName: event.payload?.tool_name,
      arguments: event.payload?.arguments || {},
      runSeq,
      createdAt: event.created_at || new Date().toISOString(),
    };
    next = {
      ...next,
      permissions: upsertByID(next.permissions, permission),
      runs: updateRunByID(next.runs, runId, {
        status: 'waiting_permission', lastEventType: event.type, lastRunSeq: runSeq,
      }),
    };
    effects.push({ type: 'upsert_global_permission', permission });
    effects.push({ type: 'select_activity_tab' });
    return { runtime: next, effects };
  }

  if (event.type === 'tool_started') {
    const tool = {
      id: event.payload?.tool_call_id || event.event_id,
      runId,
      assignmentId: event.assignment_id,
      workerId: event.worker?.id || '',
      profileKey: event.worker?.profile_key || '',
      name: event.payload?.tool_name || 'tool',
      displayName: event.payload?.display_name || event.payload?.tool_name || '工具',
      risk: event.payload?.risk || 'low',
      arguments: event.payload?.arguments || {},
      status: event.payload?.status || 'running',
      output: '', error: '', startedSeq: runSeq, runSeq,
      startedAt: event.created_at || new Date().toISOString(),
    };
    const tools = upsertByID(next.tools, tool);
    next = {
      ...next,
      tools,
      runs: updateRunByID(next.runs, runId, {
        lastEventType: event.type, lastRunSeq: runSeq,
        toolCount: countToolsForRun(tools, runId),
      }),
    };
    return { runtime: next, effects };
  }

  if (event.type === 'tool_output') {
    next = { ...next, tools: next.tools.map((item) => item.id === event.payload?.tool_call_id
      ? { ...item, output: `${item.output || ''}${event.payload?.delta || ''}`, runSeq }
      : item) };
    return { runtime: next, effects };
  }

  if (event.type === 'todo_updated') return { runtime: updateTodos(next, event.payload || {}), effects };

  if (event.type === 'goal_updated') {
    const updated = goalFromUpdatedEvent(event.payload || {});
    if (updated) {
      const goals = [...(next.goals || [])];
      const index = goals.findIndex((goal) => goal.id === updated.id);
      if (index >= 0) goals[index] = updated; else goals.unshift(updated);
      const goal = pickFocusGoal(goals);
      next = { ...next, goals, goal, goalHydrated: true,
        goalExpanded: Boolean(goal && ['active', 'paused'].includes(goal.status)) || next.goalExpanded };
    }
    return { runtime: next, effects };
  }

  if (event.type === 'tool_finished' || event.type === 'tool_failed') {
    next = {
      ...next,
      tools: next.tools.map((item) => item.id === event.payload?.tool_call_id ? {
        ...item,
        status: event.payload?.status || (event.type === 'tool_failed' ? 'failed' : 'completed'),
        output: event.payload?.output || item.output,
        error: event.payload?.error || item.error,
        durationMs: event.payload?.duration_ms,
        runSeq,
      } : item),
      runs: updateRunByID(next.runs, runId, { lastEventType: event.type, lastRunSeq: runSeq }),
    };
    if (event.type === 'tool_finished') {
      const parsed = todosFromToolFinishedPayload(event.payload || {});
      if (parsed) next = updateTodos(next, { items: parsed.items, open_count: parsed.openCount });
    }
    return { runtime: next, effects };
  }

  if (event.type === 'finish' || event.type === 'error') {
    const runEventsByRun = { ...next.runEventsByRun };
    delete runEventsByRun[runId];
    const runStatus = event.payload?.status || (event.type === 'error' ? 'failed' : 'completed');
    const finishedAt = event.created_at || new Date().toISOString();
    const runs = updateRunByID(next.runs, runId, {
      status: runStatus,
      lastEventType: event.type, lastRunSeq: runSeq,
      error: event.payload?.error || event.payload?.message || '',
      finishedAt,
    });
    const assignments = reconcileAssignmentsWithRuns(Object.values(next.assignmentsById || {}), runs);
    next = {
      ...next,
      running: false,
      currentRunId: next.currentRunId === runId ? '' : next.currentRunId,
      runEventsByRun,
      assignmentsById: Object.fromEntries(assignments.map((assignment) => [assignment.id, assignment])),
      permissions: next.permissions.map((item) => item.runId === runId && item.status === 'pending'
        ? { ...item, status: 'closed', runSeq } : item),
      runs,
    };
    effects.push({ type: 'clear_global_permissions_for_run', runId });
    return { runtime: next, effects };
  }

  const text = event.payload?.delta || event.payload?.message || '';
  if (!text || event.payload?.visibility === 'worker_private') return { runtime: next, effects };
  next = {
    ...next,
    messages: appendWorkerText(next.messages, event, text),
    runs: updateRunByID(next.runs, runId, {
      lastEventType: event.type, lastRunSeq: runSeq,
      messageCount: ((next.runs.find((item) => item.id === runId)?.messageCount) || 0) + 1,
    }),
  };
  return { runtime: next, effects };
}
