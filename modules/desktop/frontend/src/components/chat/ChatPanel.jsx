import { useMemo } from 'react';
import {
  filterMainMessages,
  filterMainPermissions,
  filterMainTools,
} from '../../lib/conversationScope.js';
import { ConversationView } from './ConversationView.jsx';

/** Main Run conversation. Worker-private traffic is not projected here. */
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
  const scoped = useMemo(() => {
    return {
      messages: filterMainMessages(messages),
      tools: filterMainTools(tools),
      permissions: filterMainPermissions(permissions),
      emptyTitle: '准备开始',
      showComposer: true,
      readOnlyHint: '',
    };
  }, [messages, tools, permissions]);

  return (
    <section className="chat-panel">
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
