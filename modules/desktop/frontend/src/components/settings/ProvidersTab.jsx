// Split from SettingsPanel.jsx (checklist R7c)
import {
  baseUrlAfterProviderChange,
  emptyProfileDraft,
  profileDraftFrom,
  providerTypeOptions,
  reasoningEffortOptions,
} from '../../lib/providerProfiles.js';
import { Check, Plus, RefreshCw, Trash2 } from 'lucide-react';
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
  const models = Array.isArray(profileDraft.models) ? profileDraft.models : [];

  function updateModel(index, key, value) {
    const previous = models[index];
    const next = models.map((item, itemIndex) => (
      itemIndex === index ? { ...item, [key]: value } : item
    ));
    updateProfileDraft('models', next);
    if (key === 'model' && profileDraft.model === previous?.model) {
      updateProfileDraft('model', value);
    }
  }

  function removeModel(index) {
    const removed = models[index];
    const next = models.filter((_, itemIndex) => itemIndex !== index);
    const ensured = next.length > 0 ? next : [{ model: '', label: '', maxTokens: '', reasoningEffort: '' }];
    updateProfileDraft('models', ensured);
    if (profileDraft.model === removed?.model) {
      updateProfileDraft('model', ensured[0].model || '');
    }
  }

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
              <div className="settings-model-editor settings-form-span" data-testid="provider-model-editor">
                <div className="settings-model-editor-header">
                  <div>
                    <strong>模型</strong>
                    <span>配置可选模型、上下文窗口和默认思考等级</span>
                  </div>
                  <Button
                    icon={<Plus size={14} />}
                    onClick={() => updateProfileDraft('models', [
                      ...models,
                      { model: '', label: '', maxTokens: '', reasoningEffort: '' },
                    ])}
                    variant="ghost"
                  >
                    添加模型
                  </Button>
                </div>
                <div className="settings-model-list">
                  {models.map((item, index) => (
                    <div className="settings-model-row" data-testid="provider-model-row" key={index}>
                      <IconButton
                        className={profileDraft.model === item.model && item.model ? 'is-selected' : ''}
                        label={profileDraft.model === item.model && item.model ? '默认模型' : '设为默认模型'}
                        onClick={() => updateProfileDraft('model', item.model)}
                        type="button"
                      >
                        <Check size={14} />
                      </IconButton>
                      <input
                        aria-label={`模型 ${index + 1} ID`}
                        onChange={(event) => updateModel(index, 'model', event.target.value)}
                        placeholder={profileDraft.provider === 'anthropic' ? 'claude-sonnet-4-5' : 'gpt-4.1-mini'}
                        type="text"
                        value={item.model}
                      />
                      <input
                        aria-label={`模型 ${index + 1} 显示名`}
                        onChange={(event) => updateModel(index, 'label', event.target.value)}
                        placeholder="显示名（可选）"
                        type="text"
                        value={item.label}
                      />
                      <input
                        aria-label={`模型 ${index + 1} 最大 Token 数`}
                        min={0}
                        onChange={(event) => updateModel(index, 'maxTokens', event.target.value)}
                        placeholder="Token 上限"
                        step={1000}
                        type="number"
                        value={item.maxTokens}
                      />
                      <SelectMenu
                        ariaLabel={`模型 ${index + 1} 默认思考等级`}
                        onChange={(value) => updateModel(index, 'reasoningEffort', value)}
                        options={reasoningEffortOptions}
                        value={item.reasoningEffort || ''}
                      />
                      <IconButton label={`删除模型 ${index + 1}`} onClick={() => removeModel(index)} type="button">
                        <Trash2 size={14} />
                      </IconButton>
                    </div>
                  ))}
                </div>
              </div>
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
                    checked={profileDraft.stream !== false}
                    onChange={(event) => updateProfileDraft('stream', event.target.checked)}
                    type="checkbox"
                  />
                  <span>使用流式</span>
                </label>
                <label className="settings-check">
                  <input
                    checked={Boolean(profileDraft.supportsVision)}
                    onChange={(event) => updateProfileDraft("supportsVision", event.target.checked)}
                    type="checkbox"
                  />
                  <span>支持视觉</span>
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
                <Button
                  disabled={!profileDraft.baseUrl || !models.some((item) => item.model.trim())}
                  loading={profileSaving}
                  onClick={saveProviderProfile}
                >
                  {settings.providerProfileId ? '保存供应商' : '创建供应商'}
                </Button>
              </div>
            </div>
          </div>
        </>
  );
}
