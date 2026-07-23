// Split from SettingsPanel.jsx (checklist R7c)
import { Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { PlugZap } from 'lucide-react';
import { emptyMcpDraft, ModuleHeader, ManagerItem, McpDiscoveryPanel } from './shared.jsx';
import { Button, IconButton } from '../ui/button.jsx';
import { EmptyState, ErrorMessage } from '../ui/feedback.jsx';
import { Field } from '../ui/field.jsx';
import { Badge } from '../ui/badge.jsx';


export function McpTab({
  visibleMcpServers,
  mcpServersLoading,
  mcpServersError,
  mcpDraft,
  mcpSaving,
  mcpError,
  mcpDiscoveryByServer,
  setMcpDraft,
  setMcpError,
  saveMcpServer,
  deleteMcpServer,
  toggleMcpServer,
  refreshMcpServers,
  discoverMcpServer,
  callMcpTool,
  mcpCallBusy = false,
}) {
  return (
    <>
          <ModuleHeader
            badge={`${visibleMcpServers.filter((item) => item.enabled !== false).length}/${visibleMcpServers.length} 已启用`}
            title="MCP 管理"
          >
            <IconButton
              disabled={mcpServersLoading || mcpSaving}
              label="刷新 MCP 服务器"
              onClick={refreshMcpServers}
            >
              <RefreshCw size={15} />
            </IconButton>
            <Button
              disabled={mcpServersLoading || mcpSaving}
              icon={<Plus size={15} />}
              onClick={() => {
                setMcpError('');
                setMcpDraft({ ...emptyMcpDraft });
              }}
              variant="soft"
            >
              新建服务器
            </Button>
          </ModuleHeader>
          <ErrorMessage className="settings-error">{mcpServersError}</ErrorMessage>
          <ErrorMessage className="settings-error">{mcpError}</ErrorMessage>
          <div className="settings-manager-grid">
            <div className="settings-manager-list" data-testid="settings-mcp-list">
              {visibleMcpServers.length === 0 ? (
                <EmptyState title={mcpServersLoading ? '正在加载 MCP 服务器' : '暂无 MCP 服务器'}>
                  {mcpServersLoading ? '请稍候。' : '添加服务器配置后可在这里统一管理。'}
                </EmptyState>
              ) : visibleMcpServers.map((server) => (
                <ManagerItem
                  active={mcpDraft?.id === server.id}
                  disabled={mcpSaving}
                  enabled={server.enabled !== false}
                  icon={PlugZap}
                  key={server.id}
                  meta={server.command}
                  name={server.name}
                  onDelete={() => deleteMcpServer(server)}
                  onSelect={() => {
                    setMcpError('');
                    setMcpDraft({ ...server });
                  }}
                  onToggle={(event) => toggleMcpServer(server, event.target.checked)}
                >
                  {server.enabled !== false ? (
                    <IconButton
                      disabled={mcpSaving || mcpDiscoveryByServer[server.id]?.loading}
                      label={`发现 ${server.name} 工具`}
                      onClick={() => discoverMcpServer(server)}
                    >
                      {mcpDiscoveryByServer[server.id]?.loading
                        ? <RefreshCw className="settings-spin" size={14} />
                        : <Search size={14} />}
                    </IconButton>
                  ) : null}
                </ManagerItem>
              ))}
            </div>
            <div className="settings-editor-panel settings-collection-editor">
              {mcpDraft ? (
                <>
                  <div className="settings-editor-title">
                    <strong>{mcpDraft.id ? '编辑 MCP 服务器' : '新建 MCP 服务器'}</strong>
                    <Badge>{mcpDraft.id ? '已同步' : '新配置'}</Badge>
                  </div>
                  <Field className="settings-row" label="名称">
                    <input
                      onChange={(event) => setMcpDraft((current) => ({ ...current, name: event.target.value }))}
                      placeholder="filesystem"
                      type="text"
                      value={mcpDraft.name}
                    />
                  </Field>
                  <Field className="settings-row" label="启动命令">
                    <input
                      onChange={(event) => setMcpDraft((current) => ({ ...current, command: event.target.value }))}
                      placeholder="npx"
                      type="text"
                      value={mcpDraft.command}
                    />
                  </Field>
                  <Field className="settings-row" label="参数">
                    <textarea
                      onChange={(event) => setMcpDraft((current) => ({ ...current, args: event.target.value }))}
                      placeholder={'-y\n@modelcontextprotocol/server-filesystem\n.'}
                      rows={4}
                      value={mcpDraft.args}
                    />
                  </Field>
                  <Field className="settings-row" label="工作目录">
                    <input
                      onChange={(event) => setMcpDraft((current) => ({ ...current, cwd: event.target.value }))}
                      placeholder="可选"
                      type="text"
                      value={mcpDraft.cwd}
                    />
                  </Field>
                  <label className="settings-check">
                    <input
                      checked={mcpDraft.enabled !== false}
                      onChange={(event) => setMcpDraft((current) => ({ ...current, enabled: event.target.checked }))}
                      type="checkbox"
                    />
                    <span>启用服务器</span>
                  </label>
                  <div className="settings-editor-actions">
                    <Button disabled={mcpSaving} onClick={() => setMcpDraft(null)} variant="ghost">取消</Button>
                    <Button
                      disabled={mcpSaving || !mcpDraft.name.trim() || !mcpDraft.command.trim()}
                      onClick={saveMcpServer}
                    >
                      {mcpSaving ? '保存中' : mcpDraft.id ? '保存服务器' : '创建服务器'}
                    </Button>
                  </div>
                  {mcpDraft.id ? (
                    <McpDiscoveryPanel
                      callBusy={mcpCallBusy}
                      onCallTool={callMcpTool}
                      state={mcpDiscoveryByServer[mcpDraft.id]}
                    />
                  ) : null}
                </>
              ) : (
                <EmptyState title="选择服务器">从左侧选择服务器，或新建一个 MCP 配置。</EmptyState>
              )}
            </div>
          </div>
        </>
  );
}
