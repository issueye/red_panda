import { ChatComposer } from './ChatComposer.jsx';
import { ChatConversation } from './ChatConversation.jsx';
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
  onRollbackMessage,
  attachments = [],
  onAttachmentsChange,
  onAddFiles,
  uploading = false,
  providerProfiles = [],
  providerProfileId = '',
  model = '',
  enableThinking = false,
  reasoningEffort = '',
  onProviderProfileChange,
  onEnableThinkingChange,
  onReasoningEffortChange,
  readOnlyHint = '',
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
  workspaceRoot = '',
  enterToSend = true,
}) {
  return (
    <section className={classNames('conversation-view', className)}>
      <ChatConversation
        emptyTitle={emptyTitle}
        messages={messages}
        onResolvePermission={showComposer ? onResolvePermission : undefined}
        onRollbackMessage={showComposer ? onRollbackMessage : undefined}
        permissions={permissions}
        running={running}
        tools={tools}
        workspaceRoot={workspaceRoot}
      />
      {showComposer ? (
        <div className="conversation-footer">
          <div className="composer-strips">
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
            attachments={attachments}
            enterToSend={enterToSend}
            onCancel={onCancel}
            onChange={onDraftChange}
            onAttachmentsChange={onAttachmentsChange}
            onAddFiles={onAddFiles}
            onProviderProfileChange={onProviderProfileChange}
            onEnableThinkingChange={onEnableThinkingChange}
            onReasoningEffortChange={onReasoningEffortChange}
            onSend={onSend}
            providerProfileId={providerProfileId}
            model={model}
            enableThinking={enableThinking}
            reasoningEffort={reasoningEffort}
            providerProfiles={providerProfiles}
            running={running}
            tokenBudgetEnabled={tokenBudgetEnabled}
            tokenDisplayRatio={tokenDisplayRatio}
            tokenMax={tokenMax}
            tokenRatio={tokenRatio}
            tokenSoftBudget={tokenSoftBudget}
            tokenUsed={tokenUsed}
            uploading={uploading}
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
