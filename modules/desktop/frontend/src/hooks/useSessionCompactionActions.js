import { useCallback, useEffect, useRef } from 'react';
import { apiJson } from '../lib/api.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import { createEmptySessionRuntime } from '../lib/sessionRuntime.js';
import { appendMessages, createSystemMessage } from './sessionActionHelpers.js';

const ACTIVE_ASSIGNMENT_STATUSES = new Set(['queued', 'running', 'waiting_permission', 'cancelling', 'paused']);

export function useSessionCompactionActions({
  currentSessionId,
  sessionRuntimesRef,
  currentSessionIdRef,
  patchRuntime,
  runSettings,
  contextTokenBudget,
  compacting,
}) {
  const autoCompactBusyRef = useRef(false);
  const autoCompactSessionRef = useRef('');

  const pauseSessionForCompact = useCallback((sessionId) => {
    const runtime = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
    const runId = runtime.currentRunId || '';
    const delegatedAssignments = Object.values(runtime.assignmentsById || {})
      .filter((item) => item?.originWorkerId && ACTIVE_ASSIGNMENT_STATUSES.has(item.status));

    if (!runId && delegatedAssignments.length === 0 && !runtime.running) {
      return { paused: false, runId: '', assignments: [] };
    }

    appendDiagnosticLog('info', '摘要前暂停运行与 Worker 工作分配', {
      source: 'compact',
      detail: {
        sessionId,
        runId,
        assignmentCount: delegatedAssignments.length,
        assignmentIds: delegatedAssignments.map((item) => item.id),
      },
    });

    return {
      paused: delegatedAssignments.length > 0,
      runId,
      assignments: delegatedAssignments.map((item) => ({ id: item.id, status: item.status })),
    };
  }, [sessionRuntimesRef]);

  const compactSession = useCallback(async ({ silent = false } = {}) => {
    const sessionId = currentSessionIdRef.current;
    if (!sessionId) return null;

    patchRuntime(sessionId, (previous) => ({ ...previous, compacting: true }));
    let pauseInfo = { paused: false, runId: '', assignments: [] };
    try {
      pauseInfo = pauseSessionForCompact(sessionId);
      const compactBody = {
        keep_tail_turns: 3,
        mode: 'auto',
        provider_profile_id: runSettings.providerProfileId || undefined,
      };
      appendDiagnosticLog('info', '开始更新会话上下文摘要（保留最近 3 轮原文）', {
        source: 'compact',
        detail: { ...compactBody, paused: pauseInfo },
      });
      const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact`, {
        method: 'POST',
        body: JSON.stringify(compactBody),
      });
      patchRuntime(sessionId, (previous) => ({
        ...previous,
        compacting: false,
        contextSummary: result.summary || null,
        contextSummaryEndSeq: Number(result.compaction?.source_end_seq) || 0,
        messages: Number(result.paused_runs) > 0
          ? appendMessages(
              previous,
              createSystemMessage('compact_resume', '上下文摘要完成，其他 Worker 已自动恢复。'),
            ).messages
          : previous.messages,
      }));
      appendDiagnosticLog('info', `会话上下文摘要已更新（${result.summary_method || 'unknown'}）`, {
        source: 'compact',
        detail: {
          sessionId,
          sourceEndSeq: result.compaction?.source_end_seq,
          keepTailTurns: result.keep_tail_turns,
          summaryMethod: result.summary_method,
          pausedRuns: result.paused_runs ?? 0,
        },
      });
      if (!silent) {
        // Manual compaction intentionally remains quiet.
      }
      return result.compaction || null;
    } catch (error) {
      patchRuntime(sessionId, (previous) => ({ ...previous, compacting: false }));
      throw error;
    }
  }, [currentSessionIdRef, patchRuntime, pauseSessionForCompact, runSettings.providerProfileId]);

  useEffect(() => {
    if (!contextTokenBudget?.autoCompact) return;
    if (!currentSessionId) return;
    if (autoCompactBusyRef.current || compacting) return;
    if (autoCompactSessionRef.current === currentSessionId) return;

    autoCompactBusyRef.current = true;
    autoCompactSessionRef.current = currentSessionId;
    compactSession({ silent: true })
      .catch(() => {
        window.setTimeout(() => {
          if (autoCompactSessionRef.current === currentSessionId) {
            autoCompactSessionRef.current = '';
          }
        }, 8000);
      })
      .finally(() => {
        autoCompactBusyRef.current = false;
      });
  }, [compacting, compactSession, contextTokenBudget?.autoCompact, currentSessionId]);

  useEffect(() => {
    if (!contextTokenBudget?.autoCompact) {
      autoCompactSessionRef.current = '';
    }
  }, [contextTokenBudget?.autoCompact]);

  return { compactSession };
}
