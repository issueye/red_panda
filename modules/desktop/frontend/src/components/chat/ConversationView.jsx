import { ChatComposer } from './ChatComposer.jsx';
import { ChatConversation } from './ChatConversation.jsx';
import { classNames } from '../../lib/format.js';

/**
 * Standalone conversation surface: timeline + optional composer.
 * Used by the main chat tab and each open subagent tab.
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
}) {
  return (
    <section className={classNames('conversation-view', className)}>
      <ChatConversation
        emptyTitle={emptyTitle}
        messages={messages}
        onResolvePermission={showComposer ? onResolvePermission : undefined}
        permissions={permissions}
        tools={tools}
      />
      {showComposer ? (
        <ChatComposer
          onCancel={onCancel}
          onChange={onDraftChange}
          onProviderProfileChange={onProviderProfileChange}
          onSend={onSend}
          providerProfileId={providerProfileId}
          providerProfiles={providerProfiles}
          running={running}
          value={draft}
        />
      ) : readOnlyHint ? (
        <div className="conversation-readonly-hint" data-testid="conversation-readonly-hint">
          {readOnlyHint}
        </div>
      ) : null}
    </section>
  );
}
