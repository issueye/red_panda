import { useCallback } from 'react';
import { parseCommand } from '../lib/commands.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import { buildRunStartOptions } from '../lib/runOptions.js';
import {
  appendMessages,
  applyStartedRunProjection,
  beginGoalContinuationProjection,
  beginRunProjection,
  createSystemMessage,
  createUserMessage,
} from './sessionActionHelpers.js';

export function useSessionRunActions({
  workspace,
  sessionRuntimesRef,
  currentSessionIdRef,
  patchRuntime,
  loadSkills,
  runSettings,
  request,
  hydrateGoals,
  continueGoal,
  cancelGoal,
  setRightPanelTab,
}) {
  const handleLocalCommand = useCallback((sessionId, text, command) => {
    patchRuntime(sessionId, (runtime) => ({
      ...appendMessages(
        runtime,
        createUserMessage(command.displayText || text),
        createSystemMessage('cmd', command.message || '指令已处理', { includeCreatedAt: true }),
      ),
      draft: '',
    }));
  }, [patchRuntime]);

  const handleCancelGoalCommand = useCallback(async (sessionId, text, command) => {
    patchRuntime(sessionId, (runtime) => ({
      ...appendMessages(runtime, createUserMessage(command.displayText || text)),
      draft: '',
    }));
    try {
      await cancelGoal();
      patchRuntime(sessionId, (runtime) => appendMessages(
        runtime,
        createSystemMessage('cmd', '已取消当前目标。', { includeCreatedAt: true }),
      ));
    } catch (error) {
      patchRuntime(sessionId, (runtime) => appendMessages(
        runtime,
        createSystemMessage('cmd_err', `取消目标失败：${error.message}`),
      ));
    }
  }, [cancelGoal, patchRuntime]);

  const handleContinueGoalCommand = useCallback(async (sessionId, text, command) => {
    patchRuntime(sessionId, (runtime) => beginGoalContinuationProjection(
      runtime,
      command.displayText || text,
    ));
    try {
      const result = await continueGoal(command.extraText || '', {
        require_permission: command.requirePermission,
      });
      const nextRunId = result?.run_id || '';
      patchRuntime(sessionId, (runtime) => ({
        ...runtime,
        running: true,
        currentRunId: nextRunId || runtime.currentRunId,
      }));
      setRightPanelTab?.('activity');
      await hydrateGoals?.(sessionId);
    } catch (error) {
      patchRuntime(sessionId, (runtime) => ({
        ...appendMessages(
          runtime,
          createSystemMessage('cmd_err', `继续目标失败：${error.message}`),
        ),
        running: false,
      }));
    }
  }, [continueGoal, hydrateGoals, patchRuntime, setRightPanelTab]);

  const startRun = useCallback(async (sessionId, text, command) => {
    const displayText = command.displayText || text;
    const inputText = command.inputText || text;
    const optionOverrides = { require_permission: command.requirePermission };
    if (command.action === 'start_goal') {
      Object.assign(optionOverrides, {
        create_goal: true,
        goal_objective: command.objective,
        goal_title: command.title,
        goal_success_criteria: command.successCriteria,
        goals_enabled: true,
      });
    }

    patchRuntime(sessionId, (runtime) => beginRunProjection(runtime, displayText));
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
        command.action === 'start_goal'
          ? `Goal 已通过指令启动 ${nextRunId || '(无 run_id)'}`
          : `运行已启动 ${nextRunId || '(无 run_id)'}`,
        {
          source: command.action === 'start_goal' ? 'goal' : 'run',
          detail: {
            sessionId,
            runId: nextRunId,
            runtimeMode: result?.runtime_mode,
            command: command.name,
            objective: command.objective,
          },
        },
      );
      let cancelAfterStart = false;
      patchRuntime(sessionId, (runtime) => {
        cancelAfterStart = Boolean(runtime.cancelRequested);
        return applyStartedRunProjection(runtime, {
        displayText,
        result,
        runSettings,
        sessionId,
        workspace,
        });
      });
      if (cancelAfterStart && nextRunId) {
        await request('run.cancel', { run_id: nextRunId, reason: 'cancelled before run start completed' }).catch(() => {});
      }
      if (command.action === 'start_goal') {
        await hydrateGoals?.(sessionId);
      }
      setRightPanelTab?.('activity');
    } catch (error) {
      appendDiagnosticLog('error', `启动运行失败：${error.message}`, {
        source: 'run',
        detail: { sessionId },
      });
      patchRuntime(sessionId, (runtime) => ({
        ...appendMessages(
          runtime,
          createSystemMessage('gateway_error', `启动运行失败：${error.message}`),
        ),
        running: false,
        currentRunId: '',
      }));
    }
  }, [hydrateGoals, loadSkills, patchRuntime, request, runSettings, setRightPanelTab, workspace]);

  const sendTask = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const runtime = sessionRuntimesRef.current[sessionId];
    const text = (runtime?.draft || '').trim();
    if (!text || !sessionId || runtime?.running || runtime?.compacting) return;

    const command = parseCommand(text);
    if (command.action === 'help' || command.action === 'error') {
      handleLocalCommand(sessionId, text, command);
      return;
    }
    if (command.action === 'cancel_goal') {
      await handleCancelGoalCommand(sessionId, text, command);
      return;
    }
    if (command.action === 'continue_goal') {
      await handleContinueGoalCommand(sessionId, text, command);
      return;
    }
    await startRun(sessionId, text, command);
  }, [
    currentSessionIdRef,
    handleCancelGoalCommand,
    handleContinueGoalCommand,
    handleLocalCommand,
    sessionRuntimesRef,
    startRun,
  ]);

  const cancelRun = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const runtime = sessionRuntimesRef.current[sessionId];
    const runId = runtime?.currentRunId
      || runtime?.runs?.find((item) => item.status === 'running' || item.status === 'waiting_permission')?.id
      || '';
    patchRuntime(sessionId, (runtime) => ({
      ...runtime,
      running: false,
      cancelRequested: true,
      currentRunId: '',
    }));
    if (runId) {
      await request('run.cancel', { run_id: runId, reason: 'cancelled by user' }).catch(() => {});
    }
  }, [currentSessionIdRef, patchRuntime, request, sessionRuntimesRef]);

  return { sendTask, cancelRun };
}
