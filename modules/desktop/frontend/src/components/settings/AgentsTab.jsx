// Split from SettingsPanel.jsx (checklist R7c)
import { Plus, RefreshCw, Trash2, Bot } from 'lucide-react';
import { AGENT_PHASE_OPTIONS, ModuleHeader, ManagerItem } from './shared.jsx';
import { Button, IconButton } from '../ui/button.jsx';
import { EmptyState, ErrorMessage } from '../ui/feedback.jsx';
import { Field } from '../ui/field.jsx';
import { SelectMenu } from '../ui/select.jsx';
import { Badge } from '../ui/badge.jsx';
import {
  agentDisplayName,
  agentPhaseLabel,
  emptyAgentDraft,
} from '../../lib/agents.js';


export function AgentsTab({
  visibleAgents,
  agentsLoading,
  agentsError,
  agentDraft,
  agentSaving,
  agentError,
  setAgentDraft,
  saveAgent,
  toggleAgent,
  deleteAgent,
  refreshAgents,
}) {
  return (
    <>
          <ModuleHeader badge={`${visibleAgents.length} 个`} title="Worker 配置">
            <IconButton
              disabled={agentsLoading || agentSaving}
              label="刷新 Worker Profile"
              onClick={refreshAgents}
            >
              <RefreshCw size={15} />
            </IconButton>
            <Button
              disabled={agentSaving}
              icon={<Plus size={15} />}
              onClick={() => {
                setAgentError('');
                setAgentDraft(emptyAgentDraft());
              }}
              variant="soft"
            >
              新建 Profile
            </Button>
          </ModuleHeader>
          <p className="settings-module-hint">
            管理 Worker 的可复用执行 Profile。Profile 描述能力与策略，不代表运行中的 Worker 槽位。
          </p>
          {agentsError || agentError ? <ErrorMessage>{agentError || agentsError}</ErrorMessage> : null}
          <div className="settings-split">
            <div className="settings-manager-list" data-testid="agents-list">
              {agentsLoading && visibleAgents.length === 0 ? (
                <EmptyState title="加载中">正在读取 Worker Profile…</EmptyState>
              ) : null}
              {!agentsLoading && visibleAgents.length === 0 ? (
                <EmptyState title="暂无 Profile">点击新建，或刷新以加载内置 Profile。</EmptyState>
              ) : null}
              {visibleAgents.map((agent) => (
                <ManagerItem
                  active={agentDraft?.id === agent.id}
                  enabled={agent.enabled !== false}
                  icon={Bot}
                  key={agent.id}
                  meta={`${agentPhaseLabel(agent.phase)} · ${agent.key}${agent.builtin ? ' · 内置' : ''}`}
                  name={agentDisplayName(agent)}
                  onDelete={() => deleteAgent(agent)}
                  onSelect={() => {
                    setAgentError('');
                    setAgentDraft(agentDraftFrom(agent));
                  }}
                  onToggle={(event) => toggleAgent(agent, event.target.checked)}
                  disabled={agentSaving}
                />
              ))}
            </div>
            <div className="settings-editor-panel settings-collection-editor">
              {agentDraft ? (
                <>
                  <div className="settings-editor-title">
                    <strong>{agentDraft.isNew ? '新建 Profile' : '编辑 Profile'}</strong>
                    <Badge>{agentDraft.builtin ? '内置' : agentDraft.isNew ? '新配置' : '自定义'}</Badge>
                  </div>
                  <Field className="settings-row" label="标识 Key" tooltip="worker.delegate 使用的 profile_key；内置不可改">
                    <input
                      disabled={!agentDraft.isNew || agentDraft.builtin}
                      onChange={(event) => setAgentDraft((c) => ({ ...c, key: event.target.value }))}
                      placeholder="my-helper"
                      type="text"
                      value={agentDraft.key}
                    />
                  </Field>
                  <Field className="settings-row" label="名称">
                    <input
                      onChange={(event) => setAgentDraft((c) => ({ ...c, name: event.target.value }))}
                      type="text"
                      value={agentDraft.name}
                    />
                  </Field>
                  <Field className="settings-row" label="中文名">
                    <input
                      onChange={(event) => setAgentDraft((c) => ({ ...c, name_zh: event.target.value }))}
                      placeholder="目标分析师"
                      type="text"
                      value={agentDraft.name_zh}
                    />
                  </Field>
                  <Field className="settings-row" label="阶段">
                    <SelectMenu
                      ariaLabel="阶段"
                      disabled={agentDraft.builtin}
                      options={AGENT_PHASE_OPTIONS}
                      value={agentDraft.phase || 'custom'}
                      onChange={(value) => setAgentDraft((c) => ({ ...c, phase: value }))}
                    />
                  </Field>
                  <Field className="settings-row" label="默认最大轮次">
                    <input
                      min={1}
                      max={48}
                      onChange={(event) => setAgentDraft((c) => ({
                        ...c,
                        default_max_turns: Number(event.target.value) || 12,
                      }))}
                      type="number"
                      value={agentDraft.default_max_turns}
                    />
                  </Field>
                  <Field className="settings-row" label="描述">
                    <textarea
                      onChange={(event) => setAgentDraft((c) => ({ ...c, description: event.target.value }))}
                      rows={2}
                      value={agentDraft.description}
                    />
                  </Field>
                  <Field className="settings-row" label="系统提示词">
                    <textarea
                      onChange={(event) => setAgentDraft((c) => ({ ...c, system_prompt: event.target.value }))}
                      rows={8}
                      value={agentDraft.system_prompt}
                    />
                  </Field>
                  <label className="settings-check">
                    <input
                      checked={agentDraft.enabled !== false}
                      onChange={(event) => setAgentDraft((c) => ({ ...c, enabled: event.target.checked }))}
                      type="checkbox"
                    />
                    <span>启用 Profile</span>
                  </label>
                  <div className="settings-editor-actions">
                    <Button disabled={agentSaving} onClick={() => setAgentDraft(null)} variant="ghost">取消</Button>
                    <Button
                      disabled={
                        agentSaving
                        || !agentDraft.name.trim()
                        || (agentDraft.isNew && !agentDraft.key.trim())
                      }
                      onClick={saveAgent}
                    >
                      {agentSaving ? '保存中' : agentDraft.isNew ? '创建 Profile' : '保存'}
                    </Button>
                  </div>
                </>
              ) : (
                <EmptyState title="选择 Profile">从左侧选择内置 Profile，或新建自定义 Profile。</EmptyState>
              )}
            </div>
          </div>
        </>
  );
}
