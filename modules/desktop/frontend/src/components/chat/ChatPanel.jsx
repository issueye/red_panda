import { X } from 'lucide-react';
import { useMemo } from 'react';
import {
  filterMainMessages,
  filterMainPermissions,
  filterMainTools,
  filterSubagentMessages,
  filterSubagentPermissions,
  filterSubagentTools,
} from '../../lib/conversationScope.js';
import { classNames } from '../../lib/format.js';
import { ConversationView } from './ConversationView.jsx';

/**
 * Chat workspace with main conversation + open subagent conversation tabs.
 */
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
  subAgents = [],
}) {
  const activeTab = conversationTabs.find((tab) => tab.id === activeConversationTab) || conversationTabs[0] || {
    id: 'main',
    kind: 'main',
    title: '主对话',
  };

  const scoped = useMemo(() => {
    if (activeTab.kind === 'subagent' && activeTab.subagentId) {
      const agent = subAgents.find((item) => item.id === activeTab.subagentId) || {
        id: activeTab.subagentId,
        name: activeTab.title,
        runId: activeTab.runId || '',
      };
      return {
        messages: filterSubagentMessages(agent, messages),
        tools: filterSubagentTools(agent, tools),
        permissions: filterSubagentPermissions(agent, permissions),
        emptyTitle: '等待子代理输出',
        showComposer: false,
        readOnlyHint: `查看子代理「${agent.name || activeTab.title}」的对话与工具轨迹（只读）`,
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
  }, [activeTab, messages, tools, permissions, subAgents]);

  return (
    <section className="chat-panel">
      <div
        aria-label="对话标签"
        className="chat-tabs"
        data-testid="chat-tabs"
        role="tablist"
      >
        {conversationTabs.map((tab) => {
          const active = tab.id === activeTab.id;
          return (
            <div
              className={classNames('chat-tab', active && 'active', tab.kind === 'subagent' && 'is-subagent')}
              key={tab.id}
            >
              <button
                aria-selected={active}
                className="chat-tab-button"
                data-testid={tab.kind === 'main' ? 'chat-tab-main' : 'chat-tab-subagent'}
                onClick={() => onSelectConversationTab?.(tab.id)}
                role="tab"
                type="button"
              >
                <span>{tab.title}</span>
                {tab.status ? <em className={`chat-tab-status status-${tab.status}`}>{tab.statusLabel || tab.status}</em> : null}
              </button>
              {tab.closable ? (
                <button
                  aria-label={`关闭 ${tab.title}`}
                  className="chat-tab-close"
                  data-testid="chat-tab-close"
                  onClick={(event) => {
                    event.stopPropagation();
                    onCloseConversationTab?.(tab.id);
                  }}
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
        messages={scoped.messages}
        onCancel={onCancel}
        onDraftChange={onDraftChange}
        onProviderProfileChange={onProviderProfileChange}
        onResolvePermission={onResolvePermission}
        onSend={onSend}
        permissions={scoped.permissions}
        providerProfileId={providerProfileId}
        providerProfiles={providerProfiles}
        readOnlyHint={scoped.readOnlyHint}
        running={running}
        showComposer={scoped.showComposer}
        tools={scoped.tools}
      />
    </section>
  );
}
