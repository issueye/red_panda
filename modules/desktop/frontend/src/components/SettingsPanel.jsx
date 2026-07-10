import { RefreshCw, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { emptyProfileDraft, profileDraftFrom } from '../lib/providerProfiles.js';
import { Button, IconButton } from './ui/button.jsx';
import { ErrorMessage } from './ui/feedback.jsx';
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
  ['in_process', '进程内'],
  ['runtime_process', '运行时进程'],
  ['process_pool', '进程池'],
];

const focusableSelector = [
  'button:not([disabled])',
  'input:not([disabled])',
  'textarea:not([disabled])',
  '[href]',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

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

export function SettingsPanel({
  open,
  settings = {},
  providerProfiles = [],
  providerProfilesLoading = false,
  providerProfilesError = '',
  onClose,
  onChange,
  onCreateProviderProfile,
  onUpdateProviderProfile,
  onDeleteProviderProfile,
  onRefreshProviderProfiles,
}) {
  const panelRef = useRef(null);
  const closeButtonRef = useRef(null);
  const previousActiveElementRef = useRef(null);
  const selectedProfile = useMemo(
    () => providerProfiles.find((item) => item.id === settings.providerProfileId) || null,
    [providerProfiles, settings.providerProfileId],
  );
  const [profileDraft, setProfileDraft] = useState(emptyProfileDraft);
  const [profileSaving, setProfileSaving] = useState(false);
  const [profileError, setProfileError] = useState('');

  useEffect(() => {
    if (open) {
      setProfileDraft(profileDraftFrom(selectedProfile));
      setProfileError('');
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
          </div>
          <IconButton label="关闭设置" onClick={onClose} ref={closeButtonRef}>
            <X size={17} />
          </IconButton>
        </header>

        <div className="settings-content">
          <SettingSelect
            label="模型服务配置"
            options={providerProfileOptions}
            settings={settings}
            settingKey="providerProfileId"
            onUpdate={(key, value) => {
              updateSetting(key, value);
              const next = providerProfiles.find((item) => item.id === value);
              setProfileDraft(profileDraftFrom(next));
            }}
          />
          <div className="settings-profile-panel">
            <div className="settings-profile-head">
              <strong>模型服务配置</strong>
              <IconButton
                disabled={providerProfilesLoading || profileSaving}
                label="刷新模型服务配置"
                onClick={onRefreshProviderProfiles}
              >
                <RefreshCw size={15} />
              </IconButton>
            </div>
            <ErrorMessage className="settings-error">{providerProfilesError}</ErrorMessage>
            <ErrorMessage className="settings-error">{profileError}</ErrorMessage>
            <Field className="settings-row" label="名称">
              <input
                onChange={(event) => updateProfileDraft('name', event.target.value)}
                placeholder="工作 OpenAI"
                type="text"
                value={profileDraft.name}
              />
            </Field>
            <Field className="settings-row" label="基础 URL">
              <input
                onChange={(event) => updateProfileDraft('baseUrl', event.target.value)}
                placeholder="https://api.openai.com"
                type="text"
                value={profileDraft.baseUrl}
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
            <Field className="settings-row" label="API 密钥">
              <input
                onChange={(event) => updateProfileDraft('apiKey', event.target.value)}
                placeholder={selectedProfile?.apiKeySet ? selectedProfile.apiKeyMasked || '已保存密钥' : '可选'}
                type="password"
                value={profileDraft.apiKey}
              />
            </Field>
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
              <Button disabled={profileSaving} onClick={() => {
                updateSetting('providerProfileId', '');
                setProfileDraft(emptyProfileDraft);
              }} variant="ghost">
                新建
              </Button>
              <Button disabled={profileSaving || !profileDraft.baseUrl} onClick={saveProviderProfile} variant="soft">
                {settings.providerProfileId ? '保存' : '创建'}
              </Button>
              <IconButton
                disabled={profileSaving || !settings.providerProfileId}
                label="删除模型服务配置"
                onClick={deleteSelectedProfile}
              >
                <Trash2 size={15} />
              </IconButton>
            </div>
          </div>

          <h2 className="settings-section-title">运行与代理</h2>
          <SettingSelect
            label="运行模式"
            options={RUNTIME_MODES}
            settings={settings}
            settingKey="runtimeMode"
            onUpdate={updateSetting}
          />
          <label className="settings-check">
            <input
              type="checkbox"
              checked={Boolean(settings?.spawnSubAgents)}
              onChange={(event) => updateSetting('spawnSubAgents', event.target.checked)}
            />
            <span>启用子代理</span>
          </label>

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

          <h2 className="settings-section-title">工具与授权</h2>
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

        <footer className="settings-footer">
          <Button onClick={onClose} variant="soft">
            关闭
          </Button>
        </footer>
      </aside>
    </div>
  );
}
