import { RefreshCw, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { emptyProfileDraft, profileDraftFrom } from '../lib/providerProfiles.js';
import { Button, IconButton } from './ui/button.jsx';

const RUNTIME_MODES = [
  ['single_core', 'Single core'],
  ['per_run_process', 'Per-run process'],
];

const TOOL_POLICIES = [
  ['risk_based', 'Risk based'],
  ['allow_all', 'Allow all'],
  ['ask_all', 'Ask all'],
  ['deny_all', 'Deny all'],
];

const PERMISSION_MODES = [
  ['strict', 'Strict'],
  ['permissive', 'Permissive'],
  ['allow_all', 'Allow all'],
  ['deny_all', 'Deny all'],
];

const SUB_AGENT_BACKENDS = [
  ['in_process', 'In process'],
  ['runtime_process', 'Runtime process'],
  ['process_pool', 'Process pool'],
];

function SettingRow({ children, label }) {
  return (
    <label className="settings-row">
      <span>{label}</span>
      {children}
    </label>
  );
}

function SettingSelect({ label, options, settings, settingKey, onUpdate }) {
  return (
    <SettingRow label={label}>
      <select
        data-testid={`settings-${settingKey}`}
        value={settings?.[settingKey] ?? options[0]?.[0] ?? ''}
        onChange={(event) => onUpdate(settingKey, event.target.value)}
      >
        {options.map(([value, text]) => (
          <option key={value} value={value}>
            {text}
          </option>
        ))}
      </select>
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
    ['', 'Environment / echo fallback'],
    ...providerProfiles.map((item) => [
      item.id,
      `${item.name}${item.isDefault ? ' (default)' : ''}`,
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

  return (
    <div className="settings-overlay" role="presentation">
      <aside className="settings-panel" aria-label="Settings" role="dialog" aria-modal="true">
        <header className="settings-header">
          <div>
            <strong>Settings</strong>
            <span>Runtime and tool controls</span>
          </div>
          <IconButton label="Close settings" onClick={onClose}>
            <X size={17} />
          </IconButton>
        </header>

        <div className="settings-content">
          <SettingSelect
            label="Provider profile"
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
              <strong>Provider profiles</strong>
              <IconButton
                disabled={providerProfilesLoading || profileSaving}
                label="Refresh provider profiles"
                onClick={onRefreshProviderProfiles}
              >
                <RefreshCw size={15} />
              </IconButton>
            </div>
            {providerProfilesError ? <p className="settings-error">{providerProfilesError}</p> : null}
            {profileError ? <p className="settings-error">{profileError}</p> : null}
            <label className="settings-row">
              <span>Name</span>
              <input
                onChange={(event) => updateProfileDraft('name', event.target.value)}
                placeholder="Work OpenAI"
                type="text"
                value={profileDraft.name}
              />
            </label>
            <label className="settings-row">
              <span>Base URL</span>
              <input
                onChange={(event) => updateProfileDraft('baseUrl', event.target.value)}
                placeholder="https://api.openai.com"
                type="text"
                value={profileDraft.baseUrl}
              />
            </label>
            <label className="settings-row">
              <span>Profile model</span>
              <input
                onChange={(event) => updateProfileDraft('model', event.target.value)}
                placeholder="gpt-4.1-mini"
                type="text"
                value={profileDraft.model}
              />
            </label>
            <label className="settings-row">
              <span>API key</span>
              <input
                onChange={(event) => updateProfileDraft('apiKey', event.target.value)}
                placeholder={selectedProfile?.apiKeySet ? selectedProfile.apiKeyMasked || 'Saved key' : 'Optional'}
                type="password"
                value={profileDraft.apiKey}
              />
            </label>
            <div className="settings-profile-flags">
              <label className="settings-check">
                <input
                  checked={Boolean(profileDraft.isDefault)}
                  onChange={(event) => updateProfileDraft('isDefault', event.target.checked)}
                  type="checkbox"
                />
                <span>Default</span>
              </label>
              <label className="settings-check">
                <input
                  checked={profileDraft.active !== false}
                  onChange={(event) => updateProfileDraft('active', event.target.checked)}
                  type="checkbox"
                />
                <span>Active</span>
              </label>
            </div>
            <div className="settings-profile-actions">
              <Button disabled={profileSaving} onClick={() => {
                updateSetting('providerProfileId', '');
                setProfileDraft(emptyProfileDraft);
              }} variant="ghost">
                New
              </Button>
              <Button disabled={profileSaving || !profileDraft.baseUrl} onClick={saveProviderProfile} variant="soft">
                {settings.providerProfileId ? 'Save' : 'Create'}
              </Button>
              <IconButton
                disabled={profileSaving || !settings.providerProfileId}
                label="Delete provider profile"
                onClick={deleteSelectedProfile}
              >
                <Trash2 size={15} />
              </IconButton>
            </div>
          </div>

          <SettingSelect
            label="Runtime mode"
            options={RUNTIME_MODES}
            settings={settings}
            settingKey="runtimeMode"
            onUpdate={updateSetting}
          />
          <SettingSelect
            label="Tool policy"
            options={TOOL_POLICIES}
            settings={settings}
            settingKey="toolPolicy"
            onUpdate={updateSetting}
          />
          <SettingSelect
            label="Permission mode"
            options={PERMISSION_MODES}
            settings={settings}
            settingKey="permissionMode"
            onUpdate={updateSetting}
          />

          <label className="settings-check">
            <input
              type="checkbox"
              checked={Boolean(settings?.spawnSubAgents)}
              onChange={(event) => updateSetting('spawnSubAgents', event.target.checked)}
            />
            <span>Spawn sub-agents</span>
          </label>

          <SettingSelect
            label="Sub-agent backend"
            options={SUB_AGENT_BACKENDS}
            settings={settings}
            settingKey="subAgentBackend"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="Model"
            placeholder="Optional"
            settings={settings}
            settingKey="model"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="Tool allowlist"
            placeholder="workspace.read_file, workspace.list"
            settings={settings}
            settingKey="toolAllowlist"
            onUpdate={updateSetting}
          />
          <SettingTextInput
            label="Tool denylist"
            placeholder="shell.exec, workspace.write_file"
            settings={settings}
            settingKey="toolDenylist"
            onUpdate={updateSetting}
          />
        </div>

        <footer className="settings-footer">
          <Button onClick={onClose} variant="soft">
            Close
          </Button>
        </footer>
      </aside>
    </div>
  );
}
