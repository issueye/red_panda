import { useCallback, useEffect, useRef } from 'react';
import { apiJson } from '../lib/api.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import { goalShouldResumeAfterCompact } from '../lib/goals.js';
import { createEmptySessionRuntime } from '../lib/sessionRuntime.js';
import { appendMessages, createSystemMessage } from './sessionActionHelpers.js';

const ACTIVE_ASSIGNMENT_STATUSES = new Set(['running', 'waiting_permission', 'cancelling']);

function mapAssignmentStatus(assignmentsById, activeStatuses, status, summary) {
  return Object.fromEntries(Object.entries(assignmentsById || {}).map(([id, item]) => [
    id,
    activeStatuses.has(item.status) ? { ...item, status, summary } : item,
  ]));
}

export function useSessionCompactionActions({
  currentSessionId,
  sessionRuntimesRef,
  currentSessionIdRef,
  patchRuntime,
  request,
  hydrateGoals,
  continueGoal,
  runSettings,
  contextTokenBudget,
  compacting,
}) {
  const autoCompactBusyRef = useRef(false);
  const autoCompactSessionRef = useRef('');

  const pauseSessionForCompact = useCallback(async (sessionId) => {
    const runtime = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
    const runId = runtime.currentRunId || '';
    const activeAssignments = Object.values(runtime.assignmentsById || {})
      .filter((item) => ACTIVE_ASSIGNMENT_STATUSES.has(item?.status));

    if (!runId && activeAssignments.length === 0 && !runtime.running) {
      return { paused: false, runId: '', assignments: 0 };
    }

    appendDiagnosticLog('info', '摘要前暂停运行与 Worker 工作分配', {
      source: 'compact',
      detail: {
        sessionId,
        runId,
        assignmentCount: activeAssignments.length,
        assignmentIds: activeAssignments.map((item) => item.id),
      },
    });

    patchRuntime(sessionId, (previous) => ({
      ...previous,
      compacting: true,
      running: false,
      assignmentsById: mapAssignmentStatus(
        previous.assignmentsById,
        new Set(['running', 'waiting_permission']),
        'cancelling',
        '摘要前暂停',
      ),
    }));

    if (runId) {
      await request('run.cancel', {
        run_id: runId,
        reason: 'session compact pause',
      }).catch(() => null);
    }

    const deadline = Date.now() + 12_000;
    while (Date.now() < deadline) {
      const latest = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
      const stillActiveAssignments = Object.values(latest.assignmentsById || {})
        .some((item) => ACTIVE_ASSIGNMENT_STATUSES.has(item?.status));
      if (!latest.running && !stillActiveAssignments) break;
      await new Promise((resolve) => window.setTimeout(resolve, 120));
    }

    patchRuntime(sessionId, (previous) => ({
      ...previous,
      running: false,
      currentRunId: '',
      compacting: true,
      assignmentsById: mapAssignmentStatus(
        previous.assignmentsById,
        ACTIVE_ASSIGNMENT_STATUSES,
        'cancelled',
        '已为上下文摘要暂停',
      ),
    }));

    return { paused: true, runId, assignments: activeAssignments.length };
  }, [patchRuntime, request, sessionRuntimesRef]);

  const compactSession = useCallback(async ({ silent = false } = {}) => {
    const sessionId = currentSessionIdRef.current;
    if (!sessionId || sessionId === 'local-design') return null;

    patchRuntime(sessionId, (previous) => ({ ...previous, compacting: true }));
    try {
      const pauseInfo = await pauseSessionForCompact(sessionId);
      const compactBody = {
        keep_tail_turns: 3,
        mode: 'auto',
        provider_profile_id: runSettings.providerProfileId || undefined,
      };
      appendDiagnosticLog('info', '开始更新会话上下文摘要（保留最近 3 轮原文）', {
        source: 'compact',
        detail: { ...compactBody, paused: pauseInfo },
      });
      const preview = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact/preview`, {
        method: 'POST',
        body: JSON.stringify(compactBody),
      });
      const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(sessionId)}/compact`, {
        method: 'POST',
        body: JSON.stringify({ ...compactBody, summary: preview.preview?.summary }),
      });
      patchRuntime(sessionId, (previous) => ({
        ...previous,
        compacting: false,
        running: false,
        contextSummary: result.summary || preview.preview?.summary || null,
        contextSummaryEndSeq: Number(result.compaction?.source_end_seq) || 0,
        messages: pauseInfo.paused
          ? appendMessages(
              previous,
              createSystemMessage('compact_pause', '已暂停当前运行并完成上下文摘要。'),
            ).messages
          : previous.messages,
      }));
      appendDiagnosticLog('info', `会话上下文摘要已更新（${preview.preview?.summary_method || 'unknown'}）`, {
        source: 'compact',
        detail: {
          sessionId,
          sourceEndSeq: result.compaction?.source_end_seq,
          keepTailTurns: preview.preview?.keep_tail_turns,
          summaryMethod: preview.preview?.summary_method,
          pausedRuns: result.paused_runs ?? preview.paused_runs ?? 0,
        },
      });
      if (!silent) {
        // Manual compaction intentionally remains quiet.
      }
      const resumedGoal = await hydrateGoals?.(sessionId);
      if (goalShouldResumeAfterCompact(resumedGoal)) {
        appendDiagnosticLog('info', `摘要完成，恢复 Goal ${resumedGoal.id}`, {
          source: 'compact',
          detail: { sessionId, goalId: resumedGoal.id },
        });
        await continueGoal?.('', { goals_enabled: true }, resumedGoal, sessionId);
        patchRuntime(sessionId, (previous) => appendMessages(
          previous,
          createSystemMessage('compact_resume', '上下文摘要完成，Goal 已自动恢复。'),
        ));
      }
      return result.compaction || null;
    } catch (error) {
      patchRuntime(sessionId, (previous) => ({ ...previous, compacting: false }));
      throw error;
    }
  }, [continueGoal, currentSessionIdRef, hydrateGoals, patchRuntime, pauseSessionForCompact, runSettings.providerProfileId]);

  useEffect(() => {
    if (!contextTokenBudget?.autoCompact) return;
    if (!currentSessionId || currentSessionId === 'local-design') return;
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
