import { displayStatus, displayWorkerProfileName } from './displayLabels.js';

const MAIN_TAB = { id: 'main', kind: 'main', title: '主对话', closable: false };

/**
 * Open or refresh a Worker conversation tab on the session runtime.
 */
export function openWorkerConversationState(runtime, assignment) {
  if (!assignment?.id) return runtime;
  const tabId = `worker:${assignment.id}`;
  const title = displayWorkerProfileName(
    assignment.profileKey || assignment.workerId || assignment.id,
  );
  const tabs = runtime.conversationTabs || [MAIN_TAB];
  const nextTab = {
    id: tabId,
    kind: 'worker',
    assignmentId: assignment.id,
    workerId: assignment.workerId || '',
    runId: assignment.runId || '',
    task: assignment.task || '',
    title,
    status: assignment.status,
    statusLabel: displayStatus(assignment.status),
    closable: true,
  };
  return {
    ...runtime,
    conversationTabs: tabs.some((tab) => tab.id === tabId)
      ? tabs.map((tab) => (tab.id === tabId ? { ...tab, ...nextTab } : tab))
      : [...tabs, nextTab],
    activeConversationTab: tabId,
  };
}

/**
 * Close a closable conversation tab; returns to main when active tab closes.
 */
export function closeConversationTabState(runtime, tabId) {
  if (!tabId || tabId === 'main') return runtime;
  return {
    ...runtime,
    conversationTabs: (runtime.conversationTabs || []).filter((tab) => tab.id !== tabId),
    activeConversationTab: runtime.activeConversationTab === tabId
      ? 'main'
      : runtime.activeConversationTab,
  };
}
