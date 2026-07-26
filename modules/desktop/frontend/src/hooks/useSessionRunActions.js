import { useCallback } from 'react';
import { parseCommand } from '../lib/commands.js';
import { appendDiagnosticLog } from '../lib/diagnosticLog.js';
import { buildRunStartOptions } from '../lib/runOptions.js';
import {
  ATTACHMENT_MAX_PER_RUN,
  formatAttachmentRunError,
  toRunStartAttachment,
  uploadAttachment,
  validateImageFile,
} from '../lib/attachments.js';
import {
  appendMessages,
  applyStartedRunProjection,
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
  setRightPanelTab,
  providerProfiles = [],
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

  // uploadAndStageFiles uploads each picked image to the session and stages it
  // in draftAttachments. Files that fail validation/upload are surfaced as
  // error chips so the user can retry/remove (docs/51 §8.1).
  const uploadAndStageFiles = useCallback(async (sessionId, files) => {
    if (!sessionId || !files?.length) return;
    patchRuntime(sessionId, (runtime) => ({ ...runtime, uploadingAttachments: true }));

    const staged = [];
    for (const file of files) {
      if (staged.length >= ATTACHMENT_MAX_PER_RUN) break;
      const validationError = validateImageFile(file);
      if (validationError) {
        staged.push({ clientKey: `err_${Date.now()}_${Math.random()}`, error: validationError, alt: file?.name });
        continue;
      }
      try {
        const dto = await uploadAttachment(sessionId, file);
        staged.push(dto);
      } catch (error) {
        staged.push({ clientKey: `err_${Date.now()}_${Math.random()}`, error: error.message, alt: file?.name });
      }
    }

    patchRuntime(sessionId, (runtime) => ({
      ...runtime,
      uploadingAttachments: false,
      draftAttachments: [...(runtime.draftAttachments || []), ...staged].slice(0, ATTACHMENT_MAX_PER_RUN),
    }));
  }, [patchRuntime]);

  const startRun = useCallback(async (sessionId, text, command, attachments = []) => {
    const displayText = command.displayText || text;
    const inputText = command.inputText || text;
    const optionOverrides = { require_permission: command.requirePermission };

    // Only attachment references that resolved to an id/path may be sent. Error
    // chips are dropped (and should already have been removed by the user).
    const displayAttachments = attachments.filter((item) => item && (item.id || item.path) && !item.error);
    const wireAttachments = displayAttachments
      .map(toRunStartAttachment)
      .filter(Boolean);

    // Client-side vision gate (docs/51 §6.5): fail fast with a localized message
    // before optimistically projecting the user bubble / calling run.start.
    if (wireAttachments.length) {
      const profileId = runSettings?.providerProfileId;
      const profile = providerProfiles.find((item) => item.id === profileId)
        || providerProfiles.find((item) => item.isDefault && item.active !== false)
        || providerProfiles.find((item) => item.active !== false)
        || null;
      if (profile && !profile.supportsVision) {
        const message = formatAttachmentRunError('provider profile does not support image input (vision_not_supported)');
        patchRuntime(sessionId, (runtime) => ({
          ...appendMessages(runtime, createSystemMessage('gateway_error', `启动运行失败：${message}`)),
          running: false,
        }));
        return;
      }
    }

    // Keep full AttachmentDTO on the optimistic user bubble so thumbs can load
    // via /attachments/:id even before history rehydrate (wire only carries refs).
    patchRuntime(sessionId, (runtime) => beginRunProjection(runtime, displayText, displayAttachments));
    loadSkills?.().catch(() => {});

    try {
      const runOptions = buildRunStartOptions(runSettings, workspace, text, optionOverrides);
      if (runOptions.log_llm_requests) {
        appendDiagnosticLog('info', '已开启 LLM 请求记录；本次运行将写入本机诊断目录', {
          source: 'run',
          detail: { sessionId, log_llm_requests: true },
        });
      }
      const input = { text: inputText };
      if (wireAttachments.length) input.attachments = wireAttachments;
      const result = await request('run.start', {
        session_id: sessionId,
        input,
        options: runOptions,
        subscribe: true,
      });
      const nextRunId = result?.run_id || '';
      appendDiagnosticLog(
        'info',
        `运行已启动 ${nextRunId || '(无 run_id)'}`,
        {
          source: 'run',
          detail: {
            sessionId,
            runId: nextRunId,
            runtimeMode: result?.runtime_mode,
            command: command.name,
            attachments: wireAttachments.length,
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
      setRightPanelTab?.('activity');
    } catch (error) {
      const friendly = formatAttachmentRunError(error.message);
      appendDiagnosticLog('error', `启动运行失败：${friendly}`, {
        source: 'run',
        detail: { sessionId, raw: error.message },
      });
      patchRuntime(sessionId, (runtime) => ({
        ...appendMessages(
          runtime,
          createSystemMessage('gateway_error', `启动运行失败：${friendly}`),
        ),
        // Keep attachments so the user can retry after a vision/size failure.
        // beginRunProjection already cleared draftAttachments — restore them.
        draftAttachments: displayAttachments,
        running: false,
        currentRunId: '',
      }));
    }
  }, [loadSkills, patchRuntime, providerProfiles, request, runSettings, setRightPanelTab, workspace]);

  const sendTask = useCallback(async () => {
    const sessionId = currentSessionIdRef.current;
    const runtime = sessionRuntimesRef.current[sessionId];
    const text = (runtime?.draft || '').trim();
    const attachments = runtime?.draftAttachments || [];
    if ((!text && attachments.length === 0) || !sessionId || runtime?.running || runtime?.compacting) return;

    const command = parseCommand(text);
    if (command.action === 'help' || command.action === 'error') {
      handleLocalCommand(sessionId, text, command);
      return;
    }
    await startRun(sessionId, text, command, attachments);
  }, [
    currentSessionIdRef,
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

  return { sendTask, cancelRun, uploadAndStageFiles };
}
