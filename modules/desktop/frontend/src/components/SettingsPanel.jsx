import {
  Blocks,
  Building2,
  Plus,
  PlugZap,
  RefreshCw,
  Search,
  SlidersHorizontal,
  Trash2,
  X,
} from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { emptyProfileDraft, profileDraftFrom } from '../lib/providerProfiles.js';
import { Badge } from './ui/badge.jsx';
import { Button, IconButton } from './ui/button.jsx';
import { useDialog } from './ui/dialog.jsx';
import { EmptyState, ErrorMessage } from './ui/feedback.jsx';
import { Field } from './ui/field.jsx';
import { SelectMenu } from './ui/select.jsx';

const RUNTIME_MODES = [
  ['single_core', '单核心'],
  ['per_run_process', '每次运行独立进程'],
];

const TOOL_POLICIES = [
  ['risk_based', '按风险处理'],
  ['allow_all', '全部允许'],
  ['ask_all', '全部询问'],
  ['deny_all', '全部拒绝'],
];

const PERMISSION_MODES = [
  ['strict', '严格'],
  ['permissive', '宽松'],
  ['allow_all', '全部允许'],
  ['deny_all', '全部拒绝'],
];

const SUB_AGENT_BACKENDS = [
  ['process_pool', '进程池（推荐）'],
  ['runtime_process', '运行时进程（一次性）'],
  ['in_process', '进程内（仅 planner）'],
];

const WEB_SEARCH_PROVIDERS = [
  ['auto', '自动（有 Tavily Key 优先用 Tavily）'],
  ['tavily', 'Tavily（推荐，需 API Key）'],
  ['duckduckgo', 'DuckDuckGo（免费，可能被墙）'],
];

const SETTINGS_TABS = [
  { id: 'providers', label: '供应商管理', shortLabel: '供应商', icon: Building2 },
  { id: 'skills', label: '技能管理', shortLabel: '技能', icon: Blocks },
  { id: 'mcp', label: 'MCP 管理', shortLabel: 'MCP', icon: PlugZap },
  { id: 'other', label: '其他设置', shortLabel: '其他', icon: SlidersHorizontal },
];

const emptySkillDraft = {
  name: '',
  description: '',
  instructions: '',
  path: '',
  isNew: true,
};

const emptyMcpDraft = {
  id: '',
  name: '',
  command: '',
  args: '',
  cwd: '',
  enabled: true,
};

const focusableSelector = [
  'button:not([disabled])',
  'input:not([disabled])',
  'textarea:not([disabled])',
  '[href]',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function localID(prefix) {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}_${crypto.randomUUID()}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(16).slice(2)}`;
}

function SettingRow({ children, label }) {
  return (
    <Field className="settings-row" label={label}>
      {children}
    </Field>
  );
}

function SettingSelect({ label, options, settings, settingKey, onUpdate }) {
  return (
    <SettingRow label={label}>
      <SelectMenu
        ariaLabel={label}
        testId={`settings-${settingKey}`}
        options={options}
        value={settings?.[settingKey] ?? options[0]?.[0] ?? ''}
        onChange={(nextValue) => onUpdate(settingKey, nextValue)}
      />
    </SettingRow>
  );
}

function SettingTextInput({ label, placeholder, settings, settingKey, onUpdate }) {
  return (
    <SettingRow label={label}>
      <input
        type="text"
        value={settings?.[settingKey] ?? ''}
        placeholder={placeholder}
        onChange={(event) => onUpdate(settingKey, event.target.value)}
      />
    </SettingRow>
  );
}

function ModuleHeader({ badge, children, title }) {
  return (
    <div className="settings-module-header">
      <div>
        <h2>{title}</h2>
        {badge ? <Badge>{badge}</Badge> : null}
      </div>
      <div className="settings-module-actions">{children}</div>
    </div>
  );
}

function ManagerItem({ active, children, disabled = false, enabled, icon: Icon, meta, name, onDelete, onSelect, onToggle }) {
  const className = [
    'settings-manager-item',
    active ? 'active' : '',
    children ? 'has-extra-action' : '',
  ].filter(Boolean).join(' ');
  return (
    <div className={className}>
      <button className="settings-manager-select" disabled={disabled} onClick={onSelect} type="button">
        <Icon aria-hidden="true" size={16} />
        <span>
          <strong>{name}</strong>
          <small>{meta}</small>
        </span>
      </button>
      {children}
      <label className="settings-switch" title={enabled ? '停用' : '启用'}>
        <input
          aria-label={`${enabled ? '停用' : '启用'} ${name}`}
          checked={enabled}
          disabled={disabled}
          onChange={onToggle}
          type="checkbox"
        />
        <span aria-hidden="true" />
      </label>
      <IconButton disabled={disabled} label={`删除 ${name}`} onClick={onDelete}>
        <Trash2 size={14} />
      </IconButton>
    </div>
  );
}

function McpDiscoveryPanel({ state }) {
  if (!state) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-empty">
        <span className="mcp-discovery-heading">可用工具</span>
        <small>使用列表中的发现按钮读取服务器信息和工具清单。</small>
      </section>
    );
  }
  if (state.loading) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-loading">
        <span className="mcp-discovery-heading">正在发现</span>
        <small>正在连接服务器并读取工具清单。</small>
      </section>
    );
  }
  if (state.error) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-error">
        <span className="mcp-discovery-heading">发现失败</span>
        <ErrorMessage>{state.error}</ErrorMessage>
      </section>
    );
  }

  const servers = state.result?.servers || [];
  return (
    <section className="mcp-discovery" data-testid="mcp-discovery-result">
      <span className="mcp-discovery-heading">可用工具</span>
      {servers.length === 0 ? <small>服务器未返回发现结果。</small> : servers.map((server, index) => {
        const healthy = ['connected', 'healthy', 'ready', 'ok', 'success'].includes(server.status);
        const info = [server.serverInfo?.name, server.serverInfo?.version].filter(Boolean).join(' ');
        return (
          <div className="mcp-discovery-server" key={`${server.name}-${index}`}>
            <div className="mcp-discovery-summary">
              <strong>{server.name || info || 'MCP 服务器'}</strong>
              <Badge tone={healthy ? 'success' : server.error ? 'danger' : 'neutral'}>
                {healthy ? '健康' : server.status}
              </Badge>
              {server.durationMs > 0 ? <small>{server.durationMs} ms</small> : null}
            </div>
            {info ? <small>{info}</small> : null}
            {server.error ? <ErrorMessage>{server.error}</ErrorMessage> : null}
            {server.stderrSummary ? <small className="mcp-discovery-stderr">{server.stderrSummary}</small> : null}
            <div className="mcp-tool-list">
              {server.tools.length === 0 ? <small>未发现工具。</small> : server.tools.map((tool) => (
                <div className="mcp-tool-item" key={tool.name}>
                  <strong>{tool.name}</strong>
                  {tool.description ? <small>{tool.description}</small> : null}
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </section>
  );
}

export function SettingsPanel({
  open,
  settings = {},
  providerProfiles = [],
  providerProfilesLoading = false,
  providerProfilesError = '',
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
  const [skillDraft, setSkillDraft] = useState(null);
  const [skillSaving, setSkillSaving] = useState(false);
  const [skillError, setSkillError] = useState('');
  const [mcpDraft, setMcpDraft] = useState(null);
  const [mcpSaving, setMcpSaving] = useState(false);
  const [mcpError, setMcpError] = useState('');

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
    <>
      <ModuleHeader badge={`${providerProfiles.length} 个配置`} title="供应商管理">
        <IconButton
          disabled={providerProfilesLoading || profileSaving}
          label="刷新供应商"
          onClick={onRefreshProviderProfiles}
        >
          <RefreshCw size={15} />
        </IconButton>
        <Button
          disabled={profileSaving}
          icon={<Plus size={15} />}
          onClick={() => {
            updateSetting('providerProfileId', '');
            setProfileDraft(emptyProfileDraft);
          }}
          variant="soft"
        >
          新建供应商
        </Button>
      </ModuleHeader>
      <SettingSelect
        label="当前供应商"
        options={providerProfileOptions}
        settings={settings}
        settingKey="providerProfileId"
        onUpdate={(key, value) => {
          updateSetting(key, value);
          const next = providerProfiles.find((item) => item.id === value);
          setProfileDraft(profileDraftFrom(next));
        }}
      />
      <div className="settings-editor-panel">
        <div className="settings-editor-title">
          <strong>{settings.providerProfileId ? '编辑供应商' : '新建供应商'}</strong>
          {selectedProfile ? (
            <Badge tone={selectedProfile.active === false ? 'neutral' : 'success'}>
              {selectedProfile.active === false ? '已停用' : '已启用'}
            </Badge>
          ) : null}
        </div>
        <ErrorMessage className="settings-error">{providerProfilesError}</ErrorMessage>
        <ErrorMessage className="settings-error">{profileError}</ErrorMessage>
        <div className="settings-form-grid">
          <Field className="settings-row" label="名称">
            <input
              onChange={(event) => updateProfileDraft('name', event.target.value)}
              placeholder="工作 OpenAI"
              type="text"
              value={profileDraft.name}
            />
          </Field>
          <Field className="settings-row" label="配置模型">
            <input
              onChange={(event) => updateProfileDraft('model', event.target.value)}
              placeholder="gpt-4.1-mini"
              type="text"
              value={profileDraft.model}
            />
          </Field>
          <Field className="settings-row settings-form-span" label="基础 URL">
            <input
              onChange={(event) => updateProfileDraft('baseUrl', event.target.value)}
              placeholder="https://api.openai.com"
              type="text"
              value={profileDraft.baseUrl}
            />
          </Field>
          <Field className="settings-row settings-form-span" label="API 密钥">
            <input
              onChange={(event) => updateProfileDraft('apiKey', event.target.value)}
              placeholder={selectedProfile?.apiKeySet ? selectedProfile.apiKeyMasked || '已保存密钥' : '可选'}
              type="password"
              value={profileDraft.apiKey}
            />
          </Field>
        </div>
        <div className="settings-editor-footer">
          <div className="settings-profile-flags">
            <label className="settings-check">
              <input
                checked={Boolean(profileDraft.isDefault)}
                onChange={(event) => updateProfileDraft('isDefault', event.target.checked)}
                type="checkbox"
              />
              <span>默认</span>
            </label>
            <label className="settings-check">
              <input
                checked={profileDraft.active !== false}
                onChange={(event) => updateProfileDraft('active', event.target.checked)}
                type="checkbox"
              />
              <span>启用</span>
            </label>
          </div>
          <div className="settings-profile-actions">
            <IconButton
              disabled={profileSaving || !settings.providerProfileId}
              label="删除供应商"
              onClick={deleteSelectedProfile}
            >
              <Trash2 size={15} />
            </IconButton>
            <Button disabled={profileSaving || !profileDraft.baseUrl} onClick={saveProviderProfile}>
              {profileSaving ? '保存中' : settings.providerProfileId ? '保存供应商' : '创建供应商'}
            </Button>
          </div>
        </div>
      </div>
    </>
  );

  const skillsContent = (
    <>
      <ModuleHeader badge={`${visibleSkills.length} 个技能`} title="技能管理">
        <IconButton
          disabled={skillsLoading || skillSaving || !workspaceRoot}
          label="刷新技能"
          onClick={refreshSkills}
        >
          <RefreshCw size={15} />
        </IconButton>
        <Button
          disabled={!workspaceRoot || skillSaving}
          icon={<Plus size={15} />}
          onClick={() => {
            setSkillError('');
            setSkillDraft({ ...emptySkillDraft });
          }}
          variant="soft"
        >
          新建技能
        </Button>
      </ModuleHeader>
      {skillsError || skillError ? (
        <ErrorMessage className="settings-error">{skillError || skillsError}</ErrorMessage>
      ) : null}
      <div className="settings-manager-grid">
        <div className="settings-manager-list" data-testid="settings-skill-list">
          {visibleSkills.length === 0 ? (
            <EmptyState title={skillsLoading ? '正在加载技能' : '暂无托管技能'}>
              {workspaceRoot
                ? '创建 skill 后会写入当前工作区 .codex/skills。'
                : '打开工作区后可管理托管技能。'}
            </EmptyState>
          ) : visibleSkills.map((skill) => (
            <ManagerItem
              active={skillDraft?.name === skill.name && !skillDraft?.isNew}
              disabled={skillSaving}
              enabled
              icon={Blocks}
              key={skill.name}
              meta={skill.path || skill.description}
              name={skill.name}
              onDelete={() => deleteSkill(skill)}
              onSelect={() => selectSkill(skill)}
              onToggle={() => {}}
            />
          ))}
        </div>
        <div className="settings-editor-panel settings-collection-editor">
          {skillDraft ? (
            <>
              <div className="settings-editor-title">
                <strong>{skillDraft.isNew ? '新建技能' : '编辑技能'}</strong>
                <Badge>工作区</Badge>
              </div>
              <Field className="settings-row" label="名称">
                <input
                  disabled={!skillDraft.isNew || skillSaving}
                  onChange={(event) => setSkillDraft((current) => ({ ...current, name: event.target.value }))}
                  placeholder="code-review"
                  type="text"
                  value={skillDraft.name}
                />
              </Field>
              {skillDraft.path ? (
                <Field className="settings-row" label="路径">
                  <input disabled readOnly type="text" value={skillDraft.path} />
                </Field>
              ) : null}
              <Field className="settings-row" label="描述">
                <textarea
                  disabled={skillSaving}
                  onChange={(event) => setSkillDraft((current) => ({ ...current, description: event.target.value }))}
                  placeholder="用于选择该技能的简短说明"
                  rows={3}
                  value={skillDraft.description}
                />
              </Field>
              <Field className="settings-row" label="指令">
                <textarea
                  disabled={skillSaving}
                  onChange={(event) => setSkillDraft((current) => ({ ...current, instructions: event.target.value }))}
                  placeholder="完整 Markdown 指令"
                  rows={8}
                  value={skillDraft.instructions}
                />
              </Field>
              <div className="settings-editor-actions">
                <Button onClick={() => setSkillDraft(null)} variant="ghost">取消</Button>
                <Button
                  disabled={
                    skillSaving
                    || !skillDraft.name.trim()
                    || !skillDraft.description.trim()
                    || !skillDraft.instructions.trim()
                    || !workspaceRoot
                  }
                  onClick={saveSkill}
                >
                  {skillSaving ? '保存中' : '保存技能'}
                </Button>
              </div>
            </>
          ) : (
            <EmptyState title="选择技能">从左侧选择技能，或新建一个托管技能。</EmptyState>
          )}
        </div>
      </div>
    </>
  );

  const mcpContent = (
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
                <McpDiscoveryPanel state={mcpDiscoveryByServer[mcpDraft.id]} />
              ) : null}
            </>
          ) : (
            <EmptyState title="选择服务器">从左侧选择服务器，或新建一个 MCP 配置。</EmptyState>
          )}
        </div>
      </div>
    </>
  );

  const otherContent = (
    <>
      <ModuleHeader title="其他设置" />
      <section className="settings-section">
        <h3>运行与代理</h3>
        <div className="settings-form-grid">
          <SettingSelect
            label="运行模式"
            options={RUNTIME_MODES}
            settings={settings}
            settingKey="runtimeMode"
            onUpdate={updateSetting}
          />
          <SettingSelect
            label="子代理后端"
            options={SUB_AGENT_BACKENDS}
            settings={settings}
            settingKey="subAgentBackend"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="模型"
            placeholder="可选"
            settings={settings}
            settingKey="model"
            onUpdate={updateSetting}
          />
        </div>
        <label className="settings-check">
          <input
            type="checkbox"
            checked={Boolean(settings?.spawnSubAgents)}
            onChange={(event) => updateSetting('spawnSubAgents', event.target.checked)}
          />
          <span>运行时自动启动 planner 子代理</span>
        </label>
        <p className="settings-hint">
          主代理可通过 <code>subagent.run</code> 从进程池派发专科子代理，并用
          <code>subagent.list</code> / <code>subagent.cancel</code> / <code>subagent.reset</code> /
          <code>subagent.pool_status</code> / <code>subagent.pool_resize</code> / <code>subagent.pool_reset</code>
          管理子代理与进程池。默认使用进程池。
        </p>
      </section>
      <section className="settings-section">
        <h3>工具与授权</h3>
        <div className="settings-form-grid">
          <SettingSelect
            label="工具策略"
            options={TOOL_POLICIES}
            settings={settings}
            settingKey="toolPolicy"
            onUpdate={updateSetting}
          />
          <SettingSelect
            label="授权模式"
            options={PERMISSION_MODES}
            settings={settings}
            settingKey="permissionMode"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="工具轮次上限"
            placeholder="12"
            settings={settings}
            settingKey="maxToolTurns"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="工具允许列表"
            placeholder="workspace.read_file, workspace.list"
            settings={settings}
            settingKey="toolAllowlist"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="工具拒绝列表"
            placeholder="shell.exec, workspace.write_file"
            settings={settings}
            settingKey="toolDenylist"
            onUpdate={updateSetting}
          />
        </div>
        <p className="settings-hint">
          工具轮次上限主要约束主代理自身。分析类子代理的轮次由主代理先 <code>workspace.stats</code> 统计文件数后，按
          <code>max_turns = file_count + 总结轮次</code> 指定（无固定上限），并传入 <code>path</code>/<code>file_count</code>。
        </p>
      </section>
      <section className="settings-section">
        <h3>网络工具</h3>
        <div className="settings-form-grid">
          <SettingSelect
            label="搜索提供商"
            options={WEB_SEARCH_PROVIDERS}
            settings={settings}
            settingKey="webSearchProvider"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="搜索结果数量"
            placeholder="8"
            settings={settings}
            settingKey="webSearchResults"
            onUpdate={updateSetting}
          />
          <div className="settings-form-span">
            <SettingTextInput
              label="Tavily API Key"
              placeholder="tvly-xxxxxxxx"
              settings={settings}
              settingKey="webTavilyApiKey"
              onUpdate={updateSetting}
            />
          </div>
          <SettingTextInput
            label="抓取大小上限(字节)"
            placeholder="2097152"
            settings={settings}
            settingKey="webFetchMaxBytes"
            onUpdate={updateSetting}
          />
          <div className="settings-form-span">
            <SettingTextInput
              label="网络代理"
              placeholder="http://127.0.0.1:7890"
              settings={settings}
              settingKey="webHttpProxy"
              onUpdate={updateSetting}
            />
          </div>
        </div>
        <p className="settings-hint">
          <code>web.search</code> 支持 Tavily 与 DuckDuckGo。
          Tavily 在国内网络更稳定，需在 <a href="https://app.tavily.com" target="_blank" rel="noreferrer">app.tavily.com</a> 申请 Key。
          也可设置环境变量 <code>RED_PANDA_TAVILY_API_KEY</code>。
          代理仅作用于 <code>web.search</code> / <code>web.fetch</code>，例如 <code>http://127.0.0.1:7890</code> 或 <code>socks5://127.0.0.1:7891</code>。
          修改后重新发送任务即可生效。
        </p>
      </section>
    </>
  );

  const activeContent = activeTab === 'providers'
    ? providerContent
    : activeTab === 'skills'
      ? skillsContent
      : activeTab === 'mcp'
        ? mcpContent
        : otherContent;
  const activeTabLabel = SETTINGS_TABS.find((tab) => tab.id === activeTab)?.label || '设置';

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
