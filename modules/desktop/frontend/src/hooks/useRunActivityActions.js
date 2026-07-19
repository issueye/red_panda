import { useCallback } from 'react';
import { normalizeRunEvent } from '../lib/activityEvents.js';
import { apiJson } from '../lib/api.js';
import {
  isCancellableAssignment,
  patchAssignmentInRuntime,
} from '../lib/assignmentActions.js';

/**
 * Run activity panel actions: load events + cancel Worker assignment (docs/47 D2).
 */
export function useRunActivityActions({
  patchCurrentRuntime,
  setMessages,
  setRunEventsByRun,
  setRunEventsLoading,
  setRunEventsError,
  request,
}) {
  const cancelAssignment = useCallback(async (assignment) => {
    if (!isCancellableAssignment(assignment)) {
      return;
    }
    patchCurrentRuntime((rt) => patchAssignmentInRuntime(rt, assignment, {
      status: 'cancelling',
      summary: '已请求取消',
    }));
    try {
      await request('worker.assignment.cancel', {
        run_id: assignment.runId,
        assignment_id: assignment.id,
      });
    } catch (error) {
      patchCurrentRuntime((rt) => patchAssignmentInRuntime(rt, assignment, {
        summary: error.message,
      }));
      setMessages((items) => [
        ...items,
        {
          id: `assignment_cancel_error_${Date.now()}`,
          role: 'assistant',
          agent: 'system',
          text: `取消 Worker 任务失败：${error.message}`,
        },
      ]);
    }
  }, [patchCurrentRuntime, request, setMessages]);

  const loadRunEvents = useCallback(async (runId) => {
    if (!runId) {
      return [];
    }
    setRunEventsLoading((items) => ({ ...items, [runId]: true }));
    setRunEventsError((items) => ({ ...items, [runId]: '' }));
    try {
      const items = await apiJson(`/api/v1/runs/${encodeURIComponent(runId)}/events`);
      const normalized = Array.isArray(items) ? items.map(normalizeRunEvent) : [];
      setRunEventsByRun((current) => ({ ...current, [runId]: normalized }));
      return normalized;
    } catch (error) {
      setRunEventsError((items) => ({ ...items, [runId]: error.message }));
      return [];
    } finally {
      setRunEventsLoading((items) => ({ ...items, [runId]: false }));
    }
  }, [setRunEventsByRun, setRunEventsError, setRunEventsLoading]);

  return { cancelAssignment, loadRunEvents };
}
