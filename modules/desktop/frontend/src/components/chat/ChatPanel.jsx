import { X } from 'lucide-react';
import { useMemo } from 'react';
import {
  filterMainMessages,
  filterMainPermissions,
  filterMainTools,
  filterWorkerMessages,
  filterWorkerPermissions,
  filterWorkerTools,
} from '../../lib/conversationScope.js';
import { classNames } from '../../lib/format.js';
import { ConversationView } from './ConversationView.jsx';

/** Main Run conversation plus read-only Worker Assignment tabs. */
export function ChatPanel({
  messages = [],
  permissions = [],
  tools = [],
  draft,
  running,
  onDraftChange,
  onSend,
  onCancel,
  onResolvePermission,
  providerProfiles = [],
  providerProfileId = '',
  onProviderProfileChange,
  conversationTabs = [],
  activeConversationTab = 'main',
  onSelectConversationTab,
  onCloseConversationTab,
  goal = null,
  goalSessionId = '',
  goalExpanded = false,
  goalLoading = false,
  goalBusy = false,
  onGoalExpandToggle,
  onGoalContinue,
  onGoalCancel,
  todos = [],
  todoOpenCount = 0,
  todosExpanded = false,
  todosLoading = false,
  onTodosExpandToggle,
  onTodosRefresh,
  tokenUsed = 0,
  tokenMax = 0,
  tokenRatio = 0,
  tokenDisplayRatio = 0,
  tokenBudgetEnabled = false,
  tokenSoftBudget = false,
}) {
  const activeTab = conversationTabs.find((tab) => tab.id === activeConversationTab)
    || conversationTabs[0]
    || { id: 'main', kind: 'main', title: '主对话' };

  const scoped = useMemo(() => {
    if (activeTab.kind === 'worker') {
      const scope = {
        assignmentId: activeTab.assignmentId || '',
        workerId: activeTab.workerId || '',
      };
      const workerMessages = filterWorkerMessages(scope, messages);
      const assignmentInput = activeTab.task ? {
        id: `worker-task:${activeTab.assignmentId || activeTab.workerId}`,
        role: 'user',
        agentLabel: '任务输入',
        assignmentId: activeTab.assignmentId || '',
        workerId: activeTab.workerId || '',
        runId: activeTab.runId || '',
        text: activeTab.task,
      } : null;
      return {
        messages: assignmentInput ? [assignmentInput, ...workerMessages] : workerMessages,
        tools: filterWorkerTools(scope, tools),
        permissions: filterWorkerPermissions(scope, permissions),
        emptyTitle: '等待 Worker 输出',
        showComposer: false,
        readOnlyHint: `查看 Worker「${activeTab.title}」的对话与工具轨迹（只读）`,
      };
    }
    return {
      messages: filterMainMessages(messages),
      tools: filterMainTools(tools),
      permissions: filterMainPermissions(permissions),
      emptyTitle: '准备开始',
      showComposer: true,
      readOnlyHint: '',
    };
  }, [activeTab, messages, tools, permissions]);

  return (
    <section className="chat-panel">
      <div aria-label="对话标签" className="chat-tabs" data-testid="chat-tabs" role="tablist">
        {conversationTabs.map((tab) => {
          const active = tab.id === activeTab.id;
          return (
            <div
              className={classNames('chat-tab', active && 'active', tab.kind === 'worker' && 'is-worker')}
              key={tab.id}
            >
              <button
                aria-selected={active}
                className="chat-tab-button"
                data-testid={tab.kind === 'main' ? 'chat-tab-main' : 'chat-tab-worker'}
                onClick={() => onSelectConversationTab?.(tab.id)}
                role="tab"
                type="button"
              >
                <span>{tab.title}</span>
                {tab.status ? (
                  <em className={`chat-tab-status status-${tab.status}`}>{tab.statusLabel || tab.status}</em>
                ) : null}
              </button>
              {tab.closable ? (
                <button
                  aria-label={`关闭 ${tab.title}`}
                  className="chat-tab-close"
                  data-testid="chat-tab-close"
                  onClick={() => onCloseConversationTab?.(tab.id)}
                  type="button"
                >
                  <X size={12} />
                </button>
              ) : null}
            </div>
          );
        })}
      </div>
      <ConversationView
        draft={draft}
        emptyTitle={scoped.emptyTitle}
        goal={goal}
        goalBusy={goalBusy}
        goalExpanded={goalExpanded}
        goalLoading={goalLoading}
        goalSessionId={goalSessionId}
        messages={scoped.messages}
        onCancel={onCancel}
        onDraftChange={onDraftChange}
        onGoalCancel={onGoalCancel}
        onGoalContinue={onGoalContinue}
        onGoalExpandToggle={onGoalExpandToggle}
        onProviderProfileChange={onProviderProfileChange}
        onResolvePermission={onResolvePermission}
        onSend={onSend}
        onTodosExpandToggle={onTodosExpandToggle}
        onTodosRefresh={onTodosRefresh}
        permissions={scoped.permissions}
        providerProfileId={providerProfileId}
        providerProfiles={providerProfiles}
        readOnlyHint={scoped.readOnlyHint}
        running={running}
        showComposer={scoped.showComposer}
        todoOpenCount={todoOpenCount}
        todos={todos}
        todosExpanded={todosExpanded}
        todosLoading={todosLoading}
        tokenBudgetEnabled={tokenBudgetEnabled}
        tokenDisplayRatio={tokenDisplayRatio}
        tokenMax={tokenMax}
        tokenRatio={tokenRatio}
        tokenSoftBudget={tokenSoftBudget}
        tokenUsed={tokenUsed}
        tools={scoped.tools}
      />
    </section>
  );
}
