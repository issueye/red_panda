/**
 * Pure reduction of Gateway run events onto a session runtime projection.
 * Side effects (global pending permissions, UI tab switches) are returned as
 * effect descriptors for the caller (checklist R7b).
 */

import { extractAgentScope } from './conversationScope.js';
import { displayStatus } from './displayLabels.js';
import { goalFromUpdatedEvent, pickFocusGoal } from './goals.js';
import { isRootTerminalRunEvent } from './runEventLifecycle.js';
import { createEmptySessionRuntime } from './sessionRuntime.js';
import { resolveSubAgentLifecycleStatus } from './subagentStatus.js';
import { todosFromToolFinishedPayload, todosFromUpdatedEvent } from './todos.js';

export function appendAgentText(items, payload, text) {
  const scope = extractAgentScope(payload);
  const agentName = scope.agentName || 'agent';
  const runId = payload.run_id || payload.root_run_id || '';
  const eventSeq = Number(payload.root_seq) || 0;
  const previous = items[items.length - 1];
  const canAppend = payload.type === 'message_delta'
    && previous?.role === 'assistant'
    && previous.agent === agentName
    && previous.runId === runId
    && (previous.subagentId || '') === (scope.subagentId || '')
    && previous.eventSeq > 0
    && eventSeq === previous.eventSeq + 1;

  if (canAppend) {
    return [
      ...items.slice(0, -1),
      {
        ...previous,
        eventSeq,
        rootSeq: eventSeq,
        text: `${previous.text}${text}`,
      },
    ];
  }

  return [
    ...items,
    {
      id: payload.event_id || payload.id || `evt_${Date.now()}`,
      role: 'assistant',
      agent: agentName,
      agentRole: scope.agentRole || '',
      subagentId: scope.subagentId || '',
      runId,
      eventSeq,
      rootSeq: eventSeq,
      createdAt: payload.created_at || new Date().toISOString(),
      text,
    },
  ];
}

export function countToolsForRun(items, runId) {
  if (!Array.isArray(items) || !runId) return 0;
  return items.filter((item) => item.rootRunId === runId).length;
}

export function updateRunByID(items, runId, patch) {
  let found = false;
  const next = items.map((item) => {
    if (item.id !== runId) return item;
    found = true;
    return { ...item, ...patch };
  });
  return found ? next : items;
}

export function upsertByID(items, nextItem) {
  return [
    ...items.filter((item) => item.id !== nextItem.id),
    nextItem,
  ];
}

/**
 * @param {ReturnType<typeof createEmptySessionRuntime>} runtime
 * @param {object} payload Gateway agent event envelope
 * @returns {{ runtime: object, effects: Array<{ type: string, [key: string]: any }> }}
 */
export function reduceRunEvent(runtime, payload) {
  const prev = runtime || createEmptySessionRuntime();
  const effects = [];
  let next = {
    ...prev,
    rootSeq: Math.max(prev.rootSeq || 1, payload.root_seq || 0),
    running: prev.running || Boolean(payload.root_run_id),
    currentRunId: prev.currentRunId || payload.root_run_id || '',
    hydrated: true,
  };

  const agent = payload.agent || {};
  if (agent.role === 'subagent' || payload.type === 'subagent_update') {
    const id = agent.subagent_id || payload.payload?.subagent_id || agent.agent_id;
    if (id) {
      const current = next.subAgents.find((item) => item.id === id);
      const nextStatus = resolveSubAgentLifecycleStatus(
        payload.type,
        payload.payload,
        current?.status,
      );
      const nextSummary = payload.type === 'subagent_update'
        ? (payload.payload?.summary || current?.summary || '')
        : (current?.summary || payload.payload?.summary || '');
      const sub = {
        id,
        role: 'subagent',
        name: payload.payload?.name || agent.name || current?.name || id,
        status: nextStatus,
        backend: payload.payload?.backend || current?.backend || 'in_process',
        rootRunId: payload.root_run_id || current?.rootRunId || '',
        runId: payload.run_id || current?.runId || '',
        parentRunId: payload.parent_run_id || current?.parentRunId || '',
        summary: nextSummary,
        seq: payload.agent_seq || current?.seq || 0,
      };
      next = {
        ...next,
        subAgents: current
          ? next.subAgents.map((item) => (item.id === id ? sub : item))
          : [...next.subAgents, sub],
        conversationTabs: next.conversationTabs.map((tab) => (
          tab.subagentId === id
            ? {
                ...tab,
                title: sub.name || tab.title,
                status: sub.status,
                statusLabel: displayStatus(sub.status),
                runId: sub.runId || tab.runId,
              }
            : tab
        )),
      };
    }
  }

  if (payload.type === 'permission_required') {
    const permissionID = payload.payload?.permission_id || `perm_${Date.now()}`;
    const scope = extractAgentScope(payload);
    const nextPermission = {
      id: permissionID,
      runId: payload.payload?.run_id || payload.root_run_id,
      sessionId: payload.session_id || '',
      status: 'pending',
      summary: payload.payload?.summary || '需要授权',
      detail: payload.payload?.detail || payload.payload?.tool_name || '系统正在等待处理决定。',
      risk: payload.payload?.risk,
      toolName: payload.payload?.tool_name,
      arguments: payload.payload?.arguments || {},
      rootSeq: payload.root_seq,
      agent: scope.agentName,
      agentRole: scope.agentRole,
      subagentId: scope.subagentId,
      createdAt: payload.created_at || new Date().toISOString(),
    };
    next = {
      ...next,
      permissions: upsertByID(next.permissions, nextPermission),
      runs: next.runs.map((item) => (
        item.id === (payload.payload?.run_id || payload.root_run_id)
          ? {
              ...item,
              status: 'waiting_permission',
              lastEventType: payload.type,
              lastRootSeq: payload.root_seq,
              updatedAt: new Date().toISOString(),
            }
          : item
      )),
    };
    effects.push({ type: 'upsert_global_permission', permission: nextPermission });
    effects.push({ type: 'select_activity_tab' });
    return { runtime: next, effects };
  }

  if (payload.type === 'tool_started') {
    const toolID = payload.payload?.tool_call_id || payload.event_id;
    const scope = extractAgentScope(payload);
    const nextTool = {
      id: toolID,
      rootRunId: payload.root_run_id,
      runId: payload.run_id || payload.root_run_id || '',
      name: payload.payload?.tool_name || 'tool',
      displayName: payload.payload?.display_name || payload.payload?.tool_name || '工具',
      risk: payload.payload?.risk || 'low',
      arguments: payload.payload?.arguments || {},
      status: payload.payload?.status || 'running',
      output: '',
      error: '',
      startedSeq: payload.root_seq,
      rootSeq: payload.root_seq,
      startedAt: payload.created_at || new Date().toISOString(),
      agent: scope.agentName,
      agentRole: scope.agentRole,
      subagentId: scope.subagentId,
    };
    const nextTools = [...next.tools.filter((item) => item.id !== toolID), nextTool];
    next = {
      ...next,
      tools: nextTools,
      runs: updateRunByID(next.runs, payload.root_run_id, {
        lastEventType: payload.type,
        lastRootSeq: payload.root_seq,
        toolCount: countToolsForRun(nextTools, payload.root_run_id),
        updatedAt: new Date().toISOString(),
      }),
    };
    return { runtime: next, effects };
  }

  if (payload.type === 'tool_output') {
    const toolID = payload.payload?.tool_call_id;
    next = {
      ...next,
      tools: next.tools.map((item) => (
        item.id === toolID
          ? { ...item, output: `${item.output || ''}${payload.payload?.delta || ''}`, rootSeq: payload.root_seq }
          : item
      )),
    };
    return { runtime: next, effects };
  }

  if (payload.type === 'todo_updated') {
    const body = payload.payload || {};
    const parsed = todosFromUpdatedEvent(body);
    const prevOpen = next.todoOpenCount || 0;
    const shouldAutoExpand =
      parsed.openCount > 0 && prevOpen === 0 && !next.todosAutoExpandedOnce;
    next = {
      ...next,
      todos: parsed.items,
      todoOpenCount: parsed.openCount,
      todosVersion: (next.todosVersion || 0) + 1,
      todosHydrated: true,
      todosExpanded: shouldAutoExpand ? true : next.todosExpanded,
      todosAutoExpandedOnce: shouldAutoExpand ? true : next.todosAutoExpandedOnce,
    };
    return { runtime: next, effects };
  }

  if (payload.type === 'goal_updated') {
    const body = payload.payload || {};
    const updated = goalFromUpdatedEvent(body);
    if (updated) {
      const goals = Array.isArray(next.goals) ? [...next.goals] : [];
      const idx = goals.findIndex((g) => g.id === updated.id);
      if (idx >= 0) goals[idx] = updated;
      else goals.unshift(updated);
      const focus = pickFocusGoal(goals);
      next = {
        ...next,
        goals,
        goal: focus,
        goalHydrated: true,
        goalExpanded: focus && (focus.status === 'active' || focus.status === 'paused')
          ? true
          : next.goalExpanded,
      };
    }
    return { runtime: next, effects };
  }

  if (payload.type === 'tool_finished' || payload.type === 'tool_failed') {
    const toolID = payload.payload?.tool_call_id;
    next = {
      ...next,
      tools: next.tools.map((item) => (
        item.id === toolID
          ? {
              ...item,
              status: payload.payload?.status || (payload.type === 'tool_failed' ? 'failed' : 'completed'),
              output: payload.payload?.output || item.output,
              error: payload.payload?.error || item.error,
              durationMs: payload.payload?.duration_ms,
              rootSeq: payload.root_seq,
            }
          : item
      )),
      runs: next.runs.map((item) => (
        item.id === payload.root_run_id
          ? {
              ...item,
              lastEventType: payload.type,
              lastRootSeq: payload.root_seq,
              updatedAt: new Date().toISOString(),
            }
          : item
      )),
    };
    if (payload.type === 'tool_finished') {
      const parsed = todosFromToolFinishedPayload(payload.payload || {});
      if (parsed) {
        const prevOpen = next.todoOpenCount || 0;
        const shouldAutoExpand =
          parsed.openCount > 0 && prevOpen === 0 && !next.todosAutoExpandedOnce;
        next = {
          ...next,
          todos: parsed.items,
          todoOpenCount: parsed.openCount,
          todosVersion: (next.todosVersion || 0) + 1,
          todosHydrated: true,
          todosExpanded: shouldAutoExpand ? true : next.todosExpanded,
          todosAutoExpandedOnce: shouldAutoExpand ? true : next.todosAutoExpandedOnce,
        };
      }
    }
    return { runtime: next, effects };
  }

  if (payload.type === 'subagent_update') {
    return { runtime: next, effects };
  }

  if (isRootTerminalRunEvent(payload)) {
    const runEventsByRun = { ...next.runEventsByRun };
    delete runEventsByRun[payload.root_run_id];
    next = {
      ...next,
      running: false,
      currentRunId: next.currentRunId === payload.root_run_id ? '' : next.currentRunId,
      runEventsByRun,
      runEventsError: { ...next.runEventsError, [payload.root_run_id]: '' },
      permissions: next.permissions.map((item) => (
        item.runId === payload.root_run_id && (item.status === 'pending' || !item.status)
          ? { ...item, status: 'closed', rootSeq: payload.root_seq }
          : item
      )),
      runs: next.runs.map((item) => (
        item.id === payload.root_run_id
          ? {
              ...item,
              status: payload.payload?.status || (payload.type === 'error' ? 'failed' : 'completed'),
              lastEventType: payload.type,
              lastRootSeq: payload.root_seq,
              error: payload.payload?.error || item.error,
              finishedAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            }
          : item
      )),
    };
    effects.push({ type: 'clear_global_permissions_for_run', runId: payload.root_run_id });
    if (payload.type === 'finish' && payload.payload?.status === 'cancelled') {
      next = {
        ...next,
        messages: [
          ...next.messages,
          {
            id: payload.event_id || `cancelled_${Date.now()}`,
            role: 'assistant',
            agent: payload.agent?.name || 'runtime',
            rootSeq: payload.root_seq,
            text: '运行已取消。',
          },
        ],
      };
    }
    return { runtime: next, effects };
  }

  if (payload.type === 'skills_injected') {
    return { runtime: next, effects };
  }

  const text = payload.payload?.delta || payload.payload?.message || '';
  if (!text) {
    return { runtime: next, effects };
  }

  next = {
    ...next,
    messages: appendAgentText(next.messages, payload, text),
    runs: next.runs.map((item) => (
      item.id === payload.root_run_id
        ? {
            ...item,
            lastEventType: payload.type,
            lastRootSeq: payload.root_seq,
            messageCount: (item.messageCount || 0) + 1,
            updatedAt: new Date().toISOString(),
          }
        : item
    )),
  };
  return { runtime: next, effects };
}
