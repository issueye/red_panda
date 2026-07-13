import { X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  agentDraftFrom,
  emptyAgentDraft,
} from '../lib/agents.js';
import {
  getDiagnosticLogs,
  subscribeDiagnosticLogs,
} from '../lib/diagnosticLog.js';
import { emptyProfileDraft, profileDraftFrom } from '../lib/providerProfiles.js';
import { IconButton } from './ui/button.jsx';
import { useDialog } from './ui/dialog.jsx';
import { AgentsTab } from './settings/AgentsTab.jsx';
import { LogsTab } from './settings/LogsTab.jsx';
import { McpTab } from './settings/McpTab.jsx';
import { OtherTab } from './settings/OtherTab.jsx';
import { ProvidersTab } from './settings/ProvidersTab.jsx';
import { SkillsTab } from './settings/SkillsTab.jsx';
import {
  SETTINGS_TABS,
  emptyMcpDraft,
  emptySkillDraft,
  focusableSelector,
  localID,
} from './settings/shared.jsx';

export function SettingsPanel({
  open,
  settings = {},
  providerProfiles = [],
  providerProfilesLoading = false,
  providerProfilesError = '',
  agents = [],
  agentsLoading = false,
  agentsError = '',
  mcpServers = [],
  mcpServersLoading = false,
  mcpServersError = '',
  mcpDiscoveryByServer = {},
  skills = [],
  skillsLoading = false,
  skillsError = '',
  workspaceRoot = '',
  onClose,
  onChange,
  onCreateProviderProfile,
  onUpdateProviderProfile,
  onDeleteProviderProfile,
  onRefreshProviderProfiles,
  onCreateAgent,
  onUpdateAgent,
  onDeleteAgent,
  onRefreshAgents,
  onCreateMcpServer,
  onUpdateMcpServer,
  onDeleteMcpServer,
  onDiscoverMcpServer,
  onRefreshMcpServers,
  onCreateSkill,
  onUpdateSkill,
  onDeleteSkill,
  onLoadSkillDetail,
  onRefreshSkills,
}) {
  const dialog = useDialog();
  const panelRef = useRef(null);
  const closeButtonRef = useRef(null);
  const previousActiveElementRef = useRef(null);
  const [activeTab, setActiveTab] = useState('providers');
  const [profileDraft, setProfileDraft] = useState(emptyProfileDraft);
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileError, setProfileError] = useState('');
  const [agentDraft, setAgentDraft] = useState(null);
  const [agentSaving, setAgentSaving] = useState(false);
  const [agentError, setAgentError] = useState('');
  const [skillDraft, setSkillDraft] = useState(null);
  const [skillSaving, setSkillSaving] = useState(false);
  const [skillError, setSkillError] = useState('');
  const [mcpDraft, setMcpDraft] = useState(null);
  const [mcpSaving, setMcpSaving] = useState(false);
  const [mcpError, setMcpError] = useState('');
  const [diagnosticLogs, setDiagnosticLogs] = useState(() => getDiagnosticLogs());
  const visibleAgents = Array.isArray(agents) ? agents : [];

  const visibleSkills = Array.isArray(skills) ? skills : [];
  const visibleMcpServers = Array.isArray(mcpServers) ? mcpServers : [];
  const selectedProfile = useMemo(
    () => providerProfiles.find((item) => item.id === settings.providerProfileId) || null,
    [providerProfiles, settings.providerProfileId],
  );

  useEffect(() => {
    if (open) {
      setProfileDraft(profileDraftFrom(selectedProfile));
      setProfileError('');
      setMcpError('');
      setSkillError('');
    }
  }, [open, selectedProfile]);

  useEffect(() => {
    if (!open || typeof document === 'undefined') {
      return undefined;
    }

    previousActiveElementRef.current = document.activeElement;
    const frame = window.requestAnimationFrame(() => {
      closeButtonRef.current?.focus();
    });

    return () => {
      window.cancelAnimationFrame(frame);
      const previous = previousActiveElementRef.current;
      if (previous && typeof previous.focus === 'function' && document.contains(previous)) {
        previous.focus();
      }
    };
  }, [open]);

  useEffect(() => {
    if (!open) return undefined;
    return subscribeDiagnosticLogs(setDiagnosticLogs);
  }, [open]);

  if (!open) {
    return null;
  }

  const updateSetting = (key, value) => {
    onChange({ ...settings, [key]: value });
  };
  const updateProfileDraft = (key, value) => {
    setProfileDraft((current) => ({ ...current, [key]: value }));
  };
  const providerProfileOptions = [
    ['', '环境变量 / echo 回退'],
    ...providerProfiles.map((item) => [
      item.id,
      `${item.name}${item.isDefault ? '（默认）' : ''}`,
    ]),
  ];

  async function saveProviderProfile() {
    setProfileSaving(true);
    setProfileError('');
    try {
      const payload = { ...profileDraft };
      if (settings.providerProfileId) {
        const updated = await onUpdateProviderProfile(settings.providerProfileId, payload);
        updateSetting('providerProfileId', updated.id);
      } else {
        const created = await onCreateProviderProfile(payload);
        updateSetting('providerProfileId', created.id);
      }
      setProfileDraft((current) => ({ ...current, apiKey: '' }));
    } catch (error) {
      setProfileError(error.message);
    } finally {
      setProfileSaving(false);
    }
  }

  async function deleteSelectedProfile() {
    if (!settings.providerProfileId) {
      return;
    }
    const profileName = selectedProfile?.name || settings.providerProfileId;
    const ok = await dialog.confirm({
      title: '删除供应商',
      message: `确定删除供应商「${profileName}」？`,
      description: '此操作不可撤销，已保存的 API Key 配置将一并移除。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-provider',
    });
    if (!ok) return;
    setProfileSaving(true);
    setProfileError('');
    try {
      await onDeleteProviderProfile(settings.providerProfileId);
      updateSetting('providerProfileId', '');
      setProfileDraft(emptyProfileDraft);
    } catch (error) {
      setProfileError(error.message);
    } finally {
      setProfileSaving(false);
    }
  }

  async function saveSkill() {
    const name = skillDraft?.name?.trim();
    const description = skillDraft?.description?.trim();
    const instructions = skillDraft?.instructions?.trim();
    if (!name || !description || !instructions) return;
    if (!workspaceRoot) {
      setSkillError('打开工作区后可管理托管技能。');
      return;
    }
    setSkillSaving(true);
    setSkillError('');
    try {
      if (skillDraft.isNew) {
        await onCreateSkill?.({ name, description, instructions });
      } else {
        await onUpdateSkill?.(name, { description, instructions });
      }
      const detail = await onLoadSkillDetail?.(name);
      setSkillDraft({
        name: detail?.name || name,
        description: detail?.description || description,
        instructions: detail?.instructions || instructions,
        path: detail?.path || skillDraft.path || `.codex/skills/${name}/SKILL.md`,
        isNew: false,
      });
    } catch (error) {
      setSkillError(error.message);
    } finally {
      setSkillSaving(false);
    }
  }

  async function selectSkill(skill) {
    setSkillError('');
    setSkillDraft({
      name: skill.name,
      description: skill.description || '',
      instructions: '',
      path: skill.path || '',
      isNew: false,
    });
    try {
      const detail = await onLoadSkillDetail?.(skill.name);
      if (detail) {
        setSkillDraft({
          name: detail.name,
          description: detail.description,
          instructions: detail.instructions,
          path: detail.path,
          isNew: false,
        });
      }
    } catch (error) {
      setSkillError(error.message);
    }
  }

  async function deleteSkill(skill) {
    const ok = await dialog.confirm({
      title: '删除技能',
      message: `确定删除技能「${skill.name}」？`,
      description: '将删除工作区内对应的技能定义文件。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-skill',
    });
    if (!ok) return;
    setSkillSaving(true);
    setSkillError('');
    try {
      await onDeleteSkill?.(skill.name);
      if (skillDraft?.name === skill.name) {
        setSkillDraft(null);
      }
    } catch (error) {
      setSkillError(error.message);
    } finally {
      setSkillSaving(false);
    }
  }

  async function refreshSkills() {
    setSkillError('');
    await onRefreshSkills?.();
  }

  async function saveMcpServer() {
    if (!mcpDraft?.name.trim() || !mcpDraft?.command.trim()) return;
    setMcpSaving(true);
    setMcpError('');
    try {
      const saved = mcpDraft.id
        ? await onUpdateMcpServer(mcpDraft.id, {
            name: mcpDraft.name,
            command: mcpDraft.command,
            args: mcpDraft.args,
            cwd: mcpDraft.cwd,
            enabled: mcpDraft.enabled,
          })
        : await onCreateMcpServer(mcpDraft);
      setMcpDraft({ ...saved });
    } catch (error) {
      setMcpError(error.message);
    } finally {
      setMcpSaving(false);
    }
  }

  async function deleteMcpServer(server) {
    const ok = await dialog.confirm({
      title: '删除 MCP 服务',
      message: `确定删除 MCP 服务「${server.name || server.id}」？`,
      description: '配置删除后不可恢复。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-mcp',
    });
    if (!ok) return;
    setMcpSaving(true);
    setMcpError('');
    try {
      await onDeleteMcpServer(server.id);
      if (mcpDraft?.id === server.id) {
        setMcpDraft(null);
      }
    } catch (error) {
      setMcpError(error.message);
    } finally {
      setMcpSaving(false);
    }
  }

  async function toggleMcpServer(server, enabled) {
    setMcpSaving(true);
    setMcpError('');
    try {
      const updated = await onUpdateMcpServer(server.id, { enabled });
      if (mcpDraft?.id === server.id) {
        setMcpDraft({ ...updated });
      }
    } catch (error) {
      setMcpError(error.message);
    } finally {
      setMcpSaving(false);
    }
  }

  async function refreshMcpServers() {
    setMcpError('');
    try {
      await onRefreshMcpServers();
    } catch (error) {
      setMcpError(error.message);
    }
  }

  async function saveAgent() {
    if (!agentDraft?.name?.trim()) return;
    if (agentDraft.isNew && !agentDraft.key?.trim()) return;
    setAgentSaving(true);
    setAgentError('');
    try {
      if (agentDraft.isNew) {
        const created = await onCreateAgent?.({
          key: agentDraft.key,
          name: agentDraft.name,
          name_zh: agentDraft.name_zh,
          phase: agentDraft.phase,
          description: agentDraft.description,
          system_prompt: agentDraft.system_prompt,
          default_max_turns: Number(agentDraft.default_max_turns) || 12,
          enabled: agentDraft.enabled !== false,
        });
        setAgentDraft(agentDraftFrom(created));
      } else {
        const updated = await onUpdateAgent?.(agentDraft.id, {
          name: agentDraft.name,
          name_zh: agentDraft.name_zh,
          phase: agentDraft.builtin ? undefined : agentDraft.phase,
          description: agentDraft.description,
          system_prompt: agentDraft.system_prompt,
          default_max_turns: Number(agentDraft.default_max_turns) || 12,
          enabled: agentDraft.enabled !== false,
        });
        setAgentDraft(agentDraftFrom(updated));
      }
    } catch (error) {
      setAgentError(error.message);
    } finally {
      setAgentSaving(false);
    }
  }

  async function toggleAgent(agent, enabled) {
    setAgentSaving(true);
    setAgentError('');
    try {
      const updated = await onUpdateAgent?.(agent.id, { enabled });
      if (agentDraft?.id === agent.id) {
        setAgentDraft(agentDraftFrom(updated));
      }
    } catch (error) {
      setAgentError(error.message);
    } finally {
      setAgentSaving(false);
    }
  }

  async function deleteAgent(agent) {
    if (agent.builtin) return;
    const ok = await dialog.confirm({
      title: '删除智能体',
      message: `确定删除自定义智能体「${agentDisplayName(agent)}」？`,
      description: '内置阶段专家不可删除；自定义配置将被移除。',
      confirmLabel: '删除',
      tone: 'danger',
      testId: 'confirm-delete-agent',
    });
    if (!ok) return;
    setAgentSaving(true);
    setAgentError('');
    try {
      await onDeleteAgent?.(agent.id);
      if (agentDraft?.id === agent.id) {
        setAgentDraft(null);
      }
    } catch (error) {
      setAgentError(error.message);
    } finally {
      setAgentSaving(false);
    }
  }

  async function refreshAgents() {
    setAgentError('');
    try {
      await onRefreshAgents?.();
    } catch (error) {
      setAgentError(error.message);
    }
  }

  async function discoverMcpServer(server) {
    setMcpError('');
    setMcpDraft({ ...server });
    try {
      await onDiscoverMcpServer(server.id);
    } catch (error) {
      setMcpError(error.message);
    }
  }

  function handleTabsKeyDown(event) {
    const currentIndex = SETTINGS_TABS.findIndex((tab) => tab.id === activeTab);
    let nextIndex = currentIndex;
    if (event.key === 'ArrowRight') nextIndex = (currentIndex + 1) % SETTINGS_TABS.length;
    else if (event.key === 'ArrowLeft') nextIndex = (currentIndex - 1 + SETTINGS_TABS.length) % SETTINGS_TABS.length;
    else if (event.key === 'Home') nextIndex = 0;
    else if (event.key === 'End') nextIndex = SETTINGS_TABS.length - 1;
    else return;

    event.preventDefault();
    const nextTab = SETTINGS_TABS[nextIndex];
    setActiveTab(nextTab.id);
    window.requestAnimationFrame(() => {
      panelRef.current?.querySelector(`[data-settings-tab="${nextTab.id}"]`)?.focus();
    });
  }

  function handleDialogKeyDown(event) {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }

    if (event.key !== 'Tab') {
      return;
    }

    const focusable = Array.from(panelRef.current?.querySelectorAll(focusableSelector) || [])
      .filter((element) => element.offsetParent !== null || element === document.activeElement);
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }


  const providerContent = (
    <ProvidersTab
      deleteSelectedProfile={deleteSelectedProfile}
      onRefreshProviderProfiles={onRefreshProviderProfiles}
      profileDraft={profileDraft}
      profileError={profileError}
      profileSaving={profileSaving}
      providerProfileOptions={providerProfileOptions}
      providerProfiles={providerProfiles}
      providerProfilesError={providerProfilesError}
      providerProfilesLoading={providerProfilesLoading}
      saveProviderProfile={saveProviderProfile}
      selectedProfile={selectedProfile}
      setProfileDraft={setProfileDraft}
      settings={settings}
      updateProfileDraft={updateProfileDraft}
      updateSetting={updateSetting}
    />
  );

  const skillsContent = (
    <SkillsTab
      deleteSkill={deleteSkill}
      refreshSkills={refreshSkills}
      saveSkill={saveSkill}
      selectSkill={selectSkill}
      setSkillDraft={setSkillDraft}
      skillDraft={skillDraft}
      skillError={skillError}
      skillSaving={skillSaving}
      skillsError={skillsError}
      skillsLoading={skillsLoading}
      visibleSkills={visibleSkills}
      workspaceRoot={workspaceRoot}
    />
  );

  const mcpContent = (
    <McpTab
      deleteMcpServer={deleteMcpServer}
      discoverMcpServer={discoverMcpServer}
      mcpDiscoveryByServer={mcpDiscoveryByServer}
      mcpDraft={mcpDraft}
      mcpError={mcpError}
      mcpSaving={mcpSaving}
      mcpServersError={mcpServersError}
      mcpServersLoading={mcpServersLoading}
      refreshMcpServers={refreshMcpServers}
      saveMcpServer={saveMcpServer}
      setMcpDraft={setMcpDraft}
      setMcpError={setMcpError}
      toggleMcpServer={toggleMcpServer}
      visibleMcpServers={visibleMcpServers}
    />
  );

  const agentsContent = (
    <AgentsTab
      agentDraft={agentDraft}
      agentError={agentError}
      agentSaving={agentSaving}
      agentsError={agentsError}
      agentsLoading={agentsLoading}
      deleteAgent={deleteAgent}
      refreshAgents={refreshAgents}
      saveAgent={saveAgent}
      setAgentDraft={setAgentDraft}
      toggleAgent={toggleAgent}
      visibleAgents={visibleAgents}
    />
  );

  const otherContent = (
    <OtherTab settings={settings} updateSetting={updateSetting} />
  );

  const logsContent = (
    <LogsTab diagnosticLogs={diagnosticLogs} setDiagnosticLogs={setDiagnosticLogs} />
  );

  return (
    <div className="settings-overlay" role="presentation">
      <aside
        aria-label="设置"
        className="settings-panel"
        onKeyDown={handleDialogKeyDown}
        ref={panelRef}
        role="dialog"
        aria-modal="true"
      >
        <header className="settings-header">
          <div>
            <strong>设置</strong>
            <span>{activeTabLabel}</span>
          </div>
          <IconButton label="关闭设置" onClick={onClose} ref={closeButtonRef}>
            <X size={17} />
          </IconButton>
        </header>

        <div aria-label="设置分类" className="settings-tabs" onKeyDown={handleTabsKeyDown} role="tablist">
          {SETTINGS_TABS.map((tab) => {
            const Icon = tab.icon;
            return (
              <button
                aria-controls="settings-tab-panel"
                aria-selected={activeTab === tab.id}
                className={activeTab === tab.id ? 'settings-tab active' : 'settings-tab'}
                data-settings-tab={tab.id}
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                role="tab"
                tabIndex={activeTab === tab.id ? 0 : -1}
                type="button"
              >
                <Icon aria-hidden="true" size={16} />
                <span>{tab.shortLabel}</span>
              </button>
            );
          })}
        </div>

        <div
          aria-label={activeTabLabel}
          className="settings-content"
          id="settings-tab-panel"
          role="tabpanel"
        >
          {activeContent}
        </div>

        <footer className="settings-footer">
          <Button onClick={onClose} variant="soft">
            关闭
          </Button>
        </footer>
      </aside>
    </div>
  );
}

