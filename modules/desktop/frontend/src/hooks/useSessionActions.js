import { useCallback, useEffect, useRef } from 'react';
import { apiJson } from '../lib/api.js';
import { parseCommand } from '../lib/commands.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import { selectDirectory } from '../lib/desktopShell.js';
import { buildRunStartOptions } from '../lib/runOptions.js';
import { createEmptySessionRuntime } from '../lib/sessionRuntime.js';
import { normalizeRun, normalizeSession } from '../lib/sessionNormalize.js';

/**
 * Session lifecycle + run actions (docs/36 C2).
 * Owns no sessionRuntimes map; mutates projection via patchRuntime / setSessionRuntimes.
 *
 * @param {object} options
 */
export function useSessionActions({
  dialog,
  sessions,
  setSessions,
  currentSessionId,
  setCurrentSessionId,
  workspace,
  setWorkspace,
  recentWorkspaces,
  setRecentWorkspaces,
  setSessionRuntimes,
  sessionRuntimes,
  sessionRuntimesRef,
  currentSessionIdRef,
  patchRuntime,
  setGlobalPendingPermissions,
  loadSessionState,
  loadGlobalPendingPermissions,
  openWorkspaceRoot,
  loadSkills,
  runSettings,
  request,
  hydrateGoals,
  continueGoal,
  cancelGoal,
  setRightPanelTab,
  contextTokenBudget,
  compacting,
}) {
  const autoCompactBusyRef = useRef(false);
  const autoCompactSessionRef = useRef('');

  const selectSession = useCallback((id) => {
    setCurrentSessionId(id);
    const target = sessions.find((item) => item.id === id);
    if (target?.workspaceRoot) {
      openWorkspaceRoot(target.workspaceRoot).catch(() => {});
    }
    // Do not wipe other sessions' live state — only hydrate if needed.
    const existing = sessionRuntimes[id];
    if (!existing?.hydrated || (!existing.running && (existing.messages || []).length === 0)) {
      loadSessionState(id);
    } else {
      loadGlobalPendingPermissions();
    }
  }, [
    loadGlobalPendingPermissions,
    loadSessionState,
    openWorkspaceRoot,
    sessionRuntimes,
    sessions,
    setCurrentSessionId,
  ]);

  const createSession = useCallback(async () => {
    const session = await apiJson('/api/v1/sessions', {
      method: 'POST',
      body: JSON.stringify({
        name: '新会话',
        workspace_root: workspace?.root_path || workspace?.root || '',
      }),
    });
    const normalized = normalizeSession(session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setSessionRuntimes((map) => ({
      ...map,
      [normalized.id]: createEmptySessionRuntime({ hydrated: true }),
    }));
    setCurrentSessionId(normalized.id);
    setGlobalPendingPermissions([]);
  }, [setCurrentSessionId, setGlobalPendingPermissions, setSessionRuntimes, setSessions, workspace]);

  const deleteSession = useCallback(async (session) => {
    if (!session?.id) return;
    const ok = await dialog.confirm({
      title: '删除会话',
      message: `确定删除会话「${session.title || session.id}」？`,
      description: '删除后无法从侧栏恢复该会话记录。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-session',
    });
    if (!ok) return;
    await apiJson(`/api/v1/sessions/${encodeURIComponent(session.id)}`, { method: 'DELETE' });
    const remaining = sessions.filter((item) => item.id !== session.id);
    setSessions(remaining);
    setSessionRuntimes((map) => {
      const next = { ...map };
      delete next[session.id];
      return next;
    });
    if (currentSessionId === session.id) {
      if (remaining.length > 0) {
        selectSession(remaining[0].id);
      } else {
        setCurrentSessionId('');
      }
    }
  }, [currentSessionId, dialog, selectSession, sessions, setCurrentSessionId, setSessionRuntimes, setSessions]);

  const deleteWorkspaceNode = useCallback(async (node) => {
    if (!node?.id || String(node.id).startsWith('path:')) return;
    const ok = await dialog.confirm({
      title: '移除工作区',
      message: `确定移除工作区「${node.name}」？`,
      description: '将从最近列表移除，并删除其下会话记录。磁盘文件不会被删除。',
      confirmLabel: '移除',
      tone: 'danger',
      testId: 'confirm-delete-workspace',
    });
    if (!ok) return;
    await apiJson(`/api/v1/workspaces/${encodeURIComponent(node.id)}?delete_sessions=1`, { method: 'DELETE' });
    const rootKey = (node.root || '').replace(/\\/g, '/').toLowerCase();
    const remaining = sessions.filter((item) => {
      const itemRoot = (item.workspaceRoot || '').replace(/\\/g, '/').toLowerCase();
      return itemRoot !== rootKey;
    });
    setSessions(remaining);
    const nextRecent = recentWorkspaces.filter((item) => item.id !== node.id);
    setRecentWorkspaces(nextRecent);
    const currentRoot = (workspace?.root_path || workspace?.root || '').replace(/\\/g, '/').toLowerCase();
    if (currentRoot === rootKey) {
      const nextWorkspace = nextRecent[0] || null;
      setWorkspace(nextWorkspace);
      loadSkills?.(nextWorkspace?.root_path || nextWorkspace?.root || '');
    }
    if (currentSessionId && !remaining.some((item) => item.id === currentSessionId)) {
      if (remaining.length > 0) {
        selectSession(remaining[0].id);
      } else {
        setCurrentSessionId('');
      }
    }
    setSessionRuntimes((map) => {
      const keep = new Set(remaining.map((item) => item.id));
      const next = {};
      for (const [id, runtime] of Object.entries(map)) {
        if (keep.has(id)) next[id] = runtime;
      }
      return next;
    });
  }, [
    currentSessionId,
    dialog,
    loadSkills,
    recentWorkspaces,
    selectSession,
    sessions,
    setCurrentSessionId,
    setRecentWorkspaces,
    setSessionRuntimes,
    setSessions,
    setWorkspace,
    workspace,
  ]);

  const browseWorkspaceDirectory = useCallback(async () => {
    const path = await selectDirectory();
    return path || '';
  }, []);

  const forkSession = useCallback(async () => {
    if (!currentSessionId) return;
    const current = sessions.find((item) => item.id === currentSessionId);
    const result = await apiJson(`/api/v1/sessions/${encodeURIComponent(currentSessionId)}/fork`, {
      method: 'POST',
      body: JSON.stringify({
        name: `${current?.title || '会话'} 的分叉`,
      }),
    });
    const normalized = normalizeSession(result.session);
    setSessions((items) => [normalized, ...items.filter((item) => item.id !== normalized.id)]);
    setCurrentSessionId(normalized.id);
    await loadSessionState(normalized.id);
  }, [currentSessionId, loadSessionState, sessions, setCurrentSessionId, setSessions]);

  /** Pause the Run before context summary; Run cancellation owns all Assignments. */
  const pauseSessionForCompact = useCallback(async (sessionId) => {
    const rt = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
    const runId = rt.currentRunId || '';
    const activeAssignments = Object.values(rt.assignmentsById || {}).filter((item) => (
      item?.status === 'running' || item?.status === 'waiting_permission' || item?.status === 'cancelling'
    ));

    if (!runId && activeAssignments.length === 0 && !rt.running) {
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

    // Mark local state as paused so the UI stops accepting sends immediately.
    patchRuntime(sessionId, (prev) => ({
      ...prev,
      compacting: true,
      running: false,
      assignmentsById: Object.fromEntries(Object.entries(prev.assignmentsById || {}).map(([id, item]) => [id,
        item.status === 'running' || item.status === 'waiting_permission'
          ? { ...item, status: 'cancelling', summary: '摘要前暂停' }
          : item])),
    }));

    if (runId) {
      await request('run.cancel', {
        run_id: runId,
        reason: 'session compact pause',
      }).catch(() => null);
    }

    // Wait briefly for finish events to land so history is stable.
    const deadline = Date.now() + 12_000;
    while (Date.now() < deadline) {
      const latest = sessionRuntimesRef.current[sessionId] || createEmptySessionRuntime();
      const stillRunning = latest.running;
      const stillActiveAssignments = Object.values(latest.assignmentsById || {}).some((item) => (
        item?.status === 'running' || item?.status === 'waiting_permission' || item?.status === 'cancelling'
      ));
      if (!stillRunning && !stillActiveAssignments) break;
      await new Promise((resolve) => window.setTimeout(resolve, 120));
    }

    patchRuntime(sessionId, (prev) => ({
      ...prev,
      running: false,
      currentRunId: '',
      compacting: true,
      assignmentsById: Object.fromEntries(Object.entries(prev.assignmentsById || {}).map(([id, item]) => [id,
        item.status === 'running' || item.status === 'waiting_permission' || item.status === 'cancelling'
          ? { ...item, status: 'cancelled', summary: '已为上下文摘要暂停' }
          : item])),
    }));

    return { paused: true, runId, assignments: activeAssignments.length };
  }, [patchRuntime, request, sessionRuntimesRef]);

  const compactSession = useCallback(async ({ silent = false } = {}) => {
    const sessionId = currentSessionIdRef.current;
    if (!sessionId || sessionId === 'local-design') return null;

    patchRuntime(sessionId, (prev) => ({ ...prev, compacting: true }));
    try {
      const pauseInfo = await pauseSessionForCompact(sessionId);

      // Keep recent conversation rounds verbatim for the model. Full UI history remains unchanged.
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
        body: JSON.stringify({
          ...compactBody,
          summary: preview.preview?.summary,
        }),
      });
      patchRuntime(sessionId, (prev) => ({
        ...prev,
        compacting: false,
        running: false,
        contextSummary: result.summary || preview.preview?.summary || null,
        contextSummaryEndSeq: Number(result.compaction?.source_end_seq) || 0,
        messages: pauseInfo.paused
          ? [
              ...prev.messages,
              {
                id: `compact_pause_${Date.now()}`,
                role: 'assistant',
                agent: 'system',
                text: '已暂停当前会话与关联 Worker Assignment，并完成上下文摘要。可继续发送下一条消息。',
              },
            ]
          : prev.messages,
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
        // Manual compact stays quiet; auto path can toast via caller.
      }
      await hydrateGoals?.(sessionId);
      return result.compaction || null;
    } catch (error) {
      patchRuntime(sessionId, (prev) => ({ ...prev, compacting: false }));
      throw error;
    }
  }, [currentSessionIdRef, hydrateGoals, patchRuntime, pauseSessionForCompact, runSettings.providerProfileId]);

  // Auto-compact when estimated context usage hits 90% of provider max_tokens.
  useEffect(() => {
    if (!contextTokenBudget?.autoCompact) return;
    if (!currentSessionId || currentSessionId === 'local-design') return;
    if (autoCompactBusyRef.current) return;
    if (compacting) return;
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

  const sendTask = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const sessionRt = sessionRuntimesRef.current[sessionId];
    const text = (sessionRt?.draft || '').trim();
    const sessionRunning = sessionRt?.running;
    const sessionCompacting = sessionRt?.compacting;
    if (!text || !sessionId || sessionRunning || sessionCompacting) return;

    const cmd = parseCommand(text);

    // Local-only commands: help / parse errors — no Gateway run.
    if (cmd.action === 'help' || cmd.action === 'error') {
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: cmd.displayText || text },
          {
            id: `cmd_${Date.now()}`,
            role: 'assistant',
            agent: 'system',
            createdAt: new Date().toISOString(),
            text: cmd.message || '指令已处理',
          },
        ],
      }));
      return;
    }

    // /goal cancel — stop goal without starting a run.
    if (cmd.action === 'cancel_goal') {
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: cmd.displayText || text },
        ],
      }));
      try {
        await cancelGoal();
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          messages: [
            ...rt.messages,
            {
              id: `cmd_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              createdAt: new Date().toISOString(),
              text: '已取消当前目标。',
            },
          ],
        }));
      } catch (error) {
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          messages: [
            ...rt.messages,
            {
              id: `cmd_err_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              text: `取消目标失败：${error.message}`,
            },
          ],
        }));
      }
      return;
    }

    // /goal continue — reuse continue endpoint.
    if (cmd.action === 'continue_goal') {
      const display = cmd.displayText || text;
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        draft: '',
        running: true,
        messages: [
          ...rt.messages,
          { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: display },
        ],
      }));
      try {
        const result = await continueGoal(cmd.extraText || '', {
          require_permission: cmd.requirePermission,
        });
        const nextRunId = result?.run_id || '';
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          running: true,
          currentRunId: nextRunId || rt.currentRunId,
        }));
        setRightPanelTab?.('activity');
        await hydrateGoals?.(sessionId);
      } catch (error) {
        patchRuntime(sessionId, (rt) => ({
          ...rt,
          running: false,
          messages: [
            ...rt.messages,
            {
              id: `cmd_err_${Date.now()}`,
              role: 'assistant',
              agent: 'system',
              text: `继续目标失败：${error.message}`,
            },
          ],
        }));
      }
      return;
    }

    // Normal run or /goal <objective> (start_goal via create_goal options).
    const displayText = cmd.displayText || text;
    const inputText = cmd.inputText || text;
    const optionOverrides = {
      require_permission: cmd.requirePermission,
    };
    if (cmd.action === 'start_goal') {
      optionOverrides.create_goal = true;
      optionOverrides.goal_objective = cmd.objective;
      optionOverrides.goal_title = cmd.title;
      optionOverrides.goal_success_criteria = cmd.successCriteria;
      optionOverrides.goals_enabled = true;
    }

    patchRuntime(sessionId, (rt) => ({
      ...rt,
      draft: '',
      running: true,
      assignmentsById: {},
      assignmentOrder: [],
      messages: [
        ...rt.messages,
        { id: `user_${Date.now()}`, role: 'user', createdAt: new Date().toISOString(), text: displayText },
      ],
    }));
    // Refresh settings skills list so the UI matches disk; runtime also reloads per conversation.
    loadSkills?.().catch(() => {});

    try {
      const runOptions = buildRunStartOptions(runSettings, workspace, text, optionOverrides);
      if (runOptions.log_llm_requests) {
        appendDiagnosticLog('info', '已开启 LLM 请求记录；本次运行将写入本机诊断目录', {
          source: 'run',
          detail: { sessionId, log_llm_requests: true },
        });
      }
      const result = await request('run.start', {
        session_id: sessionId,
        input: { text: inputText },
        options: runOptions,
        subscribe: true,
      });
      const nextRunId = result?.run_id || '';
      appendDiagnosticLog(
        'info',
        cmd.action === 'start_goal'
          ? `Goal 已通过指令启动 ${nextRunId || '(无 run_id)'}`
          : `运行已启动 ${nextRunId || '(无 run_id)'}`,
        {
          source: cmd.action === 'start_goal' ? 'goal' : 'run',
          detail: {
            sessionId,
            runId: nextRunId,
            runtimeMode: result?.runtime_mode,
            command: cmd.name,
            objective: cmd.objective,
          },
        },
      );
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        running: true,
        currentRunId: nextRunId || rt.currentRunId,
        runs: nextRunId
          ? [
              normalizeRun({
                id: nextRunId,
                session_id: sessionId,
                workspace_root: workspace?.root_path || workspace?.root || '',
                runtime_mode: result?.runtime_mode || runSettings.runtimeMode,
                status: 'running',
                input: displayText,
                last_run_seq: result?.run_seq || rt.runSeq,
                message_count: 1,
                tool_count: 0,
                started_at: new Date().toISOString(),
                updated_at: new Date().toISOString(),
              }),
              ...rt.runs.filter((item) => item.id !== nextRunId),
            ]
          : rt.runs,
      }));
      if (cmd.action === 'start_goal') {
        await hydrateGoals?.(sessionId);
      }
      setRightPanelTab?.('activity');
    } catch (error) {
      appendDiagnosticLog('error', `启动运行失败：${error.message}`, {
        source: 'run',
        detail: { sessionId },
      });
      patchRuntime(sessionId, (rt) => ({
        ...rt,
        running: false,
        currentRunId: '',
        messages: [
          ...rt.messages,
          {
            id: `gateway_error_${Date.now()}`,
            role: 'assistant',
            agent: 'system',
            text: `启动运行失败：${error.message}`,
          },
        ],
      }));
    }
  }, [
    cancelGoal,
    continueGoal,
    currentSessionIdRef,
    hydrateGoals,
    loadSkills,
    patchRuntime,
    request,
    runSettings,
    sessionRuntimesRef,
    setRightPanelTab,
    workspace,
  ]);

  const cancelRun = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const runId = sessionRuntimesRef.current[sessionId]?.currentRunId || '';
    patchRuntime(sessionId, (rt) => ({
      ...rt,
      running: false,
      currentRunId: '',
    }));
    if (runId) {
      await request('run.cancel', { run_id: runId }).catch(() => {});
    }
  }, [currentSessionIdRef, patchRuntime, request, sessionRuntimesRef]);

  return {
    createSession,
    deleteSession,
    deleteWorkspaceNode,
    browseWorkspaceDirectory,
    forkSession,
    compactSession,
    selectSession,
    sendTask,
    cancelRun,
  };
}
