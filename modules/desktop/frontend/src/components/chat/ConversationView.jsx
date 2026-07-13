import { ChatComposer } from './ChatComposer.jsx';
import { ChatConversation } from './ChatConversation.jsx';
import { GoalComposerStrip } from './GoalComposerStrip.jsx';
import { TodoComposerStrip } from './TodoComposerStrip.jsx';
import { classNames } from '../../lib/format.js';

/**
 * Standalone conversation surface: timeline + optional composer.
 * Used by the main Run conversation.
 */
export function ConversationView({
  className,
  emptyTitle = '准备开始',
  messages = [],
  permissions = [],
  tools = [],
  showComposer = false,
  draft = '',
  running = false,
  onDraftChange,
  onSend,
  onCancel,
  onResolvePermission,
  providerProfiles = [],
  providerProfileId = '',
  onProviderProfileChange,
  readOnlyHint = '',
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
  return (
    <section className={classNames('conversation-view', className)}>
      <ChatConversation
        emptyTitle={emptyTitle}
        messages={messages}
        onResolvePermission={showComposer ? onResolvePermission : undefined}
        permissions={permissions}
        running={running}
        tools={tools}
      />
      {showComposer ? (
        <div className="conversation-footer">
          <div className="composer-strips">
            <GoalComposerStrip
              busy={goalBusy}
              expanded={goalExpanded}
              goal={goal}
              loading={goalLoading}
              onCancel={onGoalCancel}
              onContinue={onGoalContinue}
              onToggleExpanded={onGoalExpandToggle}
              sessionId={goalSessionId}
            />
            <TodoComposerStrip
              expanded={todosExpanded}
              items={todos}
              loading={todosLoading}
              onRefresh={onTodosRefresh}
              onToggleExpanded={onTodosExpandToggle}
              openCount={todoOpenCount}
            />
          </div>
          <ChatComposer
            onCancel={onCancel}
            onChange={onDraftChange}
            onProviderProfileChange={onProviderProfileChange}
            onSend={onSend}
            providerProfileId={providerProfileId}
            providerProfiles={providerProfiles}
            running={running}
            tokenBudgetEnabled={tokenBudgetEnabled}
            tokenDisplayRatio={tokenDisplayRatio}
            tokenMax={tokenMax}
            tokenRatio={tokenRatio}
            tokenSoftBudget={tokenSoftBudget}
            tokenUsed={tokenUsed}
            value={draft}
          />
        </div>
      ) : readOnlyHint ? (
        <div className="conversation-readonly-hint" data-testid="conversation-readonly-hint">
          {readOnlyHint}
        </div>
      ) : null}
    </section>
  );
}
