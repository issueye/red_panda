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
          <ModuleHeader badge={`${visibleAgents.length} 个`} title="智能体管理">
            <IconButton
              disabled={agentsLoading || agentSaving}
              label="刷新智能体"
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
              新建智能体
            </Button>
          </ModuleHeader>
          <p className="settings-module-hint">
            管理 Goal 流水线阶段专家（分析 / 规划 / 实施 / 验证 / 终评）与自定义智能体。内置专家可停用或改提示词，不可删除。
          </p>
          {agentsError || agentError ? <ErrorMessage>{agentError || agentsError}</ErrorMessage> : null}
          <div className="settings-split">
            <div className="settings-manager-list" data-testid="agents-list">
              {agentsLoading && visibleAgents.length === 0 ? (
                <EmptyState title="加载中">正在读取智能体定义…</EmptyState>
              ) : null}
              {!agentsLoading && visibleAgents.length === 0 ? (
                <EmptyState title="暂无智能体">点击新建，或刷新以加载内置阶段专家。</EmptyState>
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
                    <strong>{agentDraft.isNew ? '新建智能体' : '编辑智能体'}</strong>
                    <Badge>{agentDraft.builtin ? '内置' : agentDraft.isNew ? '新配置' : '自定义'}</Badge>
                  </div>
                  <Field className="settings-row" label="标识 Key" tooltip="subagent.run 的 name；内置不可改">
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
                    <span>启用智能体</span>
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
                      {agentSaving ? '保存中' : agentDraft.isNew ? '创建智能体' : '保存'}
                    </Button>
                  </div>
                </>
              ) : (
                <EmptyState title="选择智能体">从左侧选择内置阶段专家，或新建自定义智能体。</EmptyState>
              )}
            </div>
          </div>
        </>
  );
}
