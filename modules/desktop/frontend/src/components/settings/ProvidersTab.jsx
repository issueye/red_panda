// Split from SettingsPanel.jsx (checklist R7c)
import {
  baseUrlAfterProviderChange,
  emptyProfileDraft,
  profileDraftFrom,
  providerTypeOptions,
} from '../../lib/providerProfiles.js';
import { Plus, RefreshCw, Trash2 } from 'lucide-react';
import { Button, IconButton } from '../ui/button.jsx';
import { ErrorMessage } from '../ui/feedback.jsx';
import { Field } from '../ui/field.jsx';
import { SelectMenu } from '../ui/select.jsx';
import {
  ModuleHeader,
  SettingSelect,
  SettingTextInput,
  SettingCheck,
  ManagerItem,
} from './shared.jsx';
import { Badge } from '../ui/badge.jsx';


export function ProvidersTab({
  providerProfiles,
  providerProfilesLoading,
  providerProfilesError,
  profileDraft,
  profileSaving,
  profileError,
  providerProfileOptions,
  selectedProfile,
  settings,
  updateSetting,
  updateProfileDraft,
  setProfileDraft,
  saveProviderProfile,
  deleteSelectedProfile,
  onRefreshProviderProfiles,
}) {
  return (
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
              <Field className="settings-row" label="供应商类型">
                <SelectMenu
                  ariaLabel="供应商类型"
                  onChange={(provider) => {
                    updateProfileDraft('provider', provider);
                    updateProfileDraft('baseUrl', baseUrlAfterProviderChange(profileDraft.baseUrl, provider));
                  }}
                  options={providerTypeOptions}
                  testId="provider-type"
                  value={profileDraft.provider}
                />
              </Field>
              <Field className="settings-row" label="配置模型">
                <input
                  onChange={(event) => updateProfileDraft('model', event.target.value)}
                  placeholder={profileDraft.provider === 'anthropic' ? 'claude-sonnet-4-5' : 'gpt-4.1-mini'}
                  type="text"
                  value={profileDraft.model}
                />
              </Field>
              <Field
                className="settings-row"
                label="最大 Token 数"
                tooltip="上下文窗口预算，用于输入框旁进度环。达到 90% 时自动更新同一会话的上下文摘要。留空表示不限制。"
              >
                <input
                  data-testid="provider-max-tokens"
                  min={0}
                  onChange={(event) => updateProfileDraft('maxTokens', event.target.value)}
                  placeholder="例如 128000"
                  step={1000}
                  type="number"
                  value={profileDraft.maxTokens}
                />
              </Field>
              <Field className="settings-row settings-form-span" label="基础 URL">
                <input
                  onChange={(event) => updateProfileDraft('baseUrl', event.target.value)}
                  placeholder={profileDraft.provider === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.openai.com'}
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
}
