import { ChatComposer } from './ChatComposer.jsx';
import { ChatConversation } from './ChatConversation.jsx';

export function ChatPanel({
  messages,
  permissions,
  tools,
  draft,
  running,
  onDraftChange,
  onSend,
  onCancel,
  onResolvePermission,
  providerProfiles = [],
  providerProfileId = '',
  onProviderProfileChange,
}) {
  return (
    <section className="chat-panel">
      <ChatConversation
        messages={messages}
        onResolvePermission={onResolvePermission}
        permissions={permissions}
        tools={tools}
      />
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
    </section>
  );
}
