export const emptyProfileDraft = {
  name: '',
  provider: 'openai_compatible',
  baseUrl: 'https://api.openai.com',
  model: '',
  maxTokens: '',
  models: [{ model: '', label: '', maxTokens: '', reasoningEffort: '' }],
  apiKey: '',
  isDefault: false,
  stream: true,
  active: true,
  supportsVision: false,
};

export const reasoningEffortOptions = [
  { value: '', label: '模型默认' },
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
  { value: 'xhigh', label: '极高' },
];

export const providerTypeOptions = [
  { value: 'openai_compatible', label: 'OpenAI Chat 兼容' },
  { value: 'openai_responses', label: 'OpenAI Responses' },
  { value: 'anthropic', label: 'Anthropic Messages' },
];

const providerDefaultUrls = {
  openai_compatible: 'https://api.openai.com',
  openai_responses: 'https://api.openai.com',
  anthropic: 'https://api.anthropic.com',
};

export function providerDefaultBaseUrl(provider) {
  return providerDefaultUrls[provider] || '';
}

export function baseUrlAfterProviderChange(currentUrl, nextProvider) {
  const knownDefaults = new Set(Object.values(providerDefaultUrls));
  return !currentUrl || knownDefaults.has(currentUrl) ? providerDefaultBaseUrl(nextProvider) : currentUrl;
}

export function normalizeProviderProfile(item) {
  const legacyModel = item.model || '';
  const legacyMaxTokens = Number(item.max_tokens) > 0 ? Number(item.max_tokens) : 0;
  const rawModels = Array.isArray(item.models) && item.models.length > 0
    ? item.models
    : legacyModel ? [{ model: legacyModel, max_tokens: legacyMaxTokens }] : [];
  const models = rawModels.map((model) => ({
    model: model.model || '',
    label: model.label || '',
    maxTokens: Number(model.max_tokens) > 0 ? Number(model.max_tokens) : 0,
    reasoningEffort: model.reasoning_effort || '',
  })).filter((model) => model.model);
  return {
    id: item.id,
    name: item.name || item.id,
    provider: item.provider || 'openai_compatible',
    baseUrl: item.base_url || '',
    model: legacyModel || models[0]?.model || '',
    maxTokens: legacyMaxTokens || models[0]?.maxTokens || 0,
    models,
    apiKeySet: Boolean(item.api_key_set),
    apiKeyMasked: item.api_key_masked || item.masked_api_key || item.api_key_preview || '',
    isDefault: Boolean(item.is_default),
    stream: item.stream !== false,
    active: item.active !== false,
    supportsVision: Boolean(item.supports_vision),
    createdAt: item.created_at,
    updatedAt: item.updated_at,
  };
}

export function profileDraftFrom(profile) {
  if (!profile) {
    return emptyProfileDraft;
  }
  return {
    name: profile.name || '',
    provider: profile.provider || 'openai_compatible',
    baseUrl: profile.baseUrl || '',
    model: profile.model || '',
    maxTokens: profile.maxTokens > 0 ? String(profile.maxTokens) : '',
    models: (profile.models?.length ? profile.models : [{
      model: profile.model || '', maxTokens: profile.maxTokens || 0,
    }]).map((model) => ({
      model: model.model || '',
      label: model.label || '',
      maxTokens: model.maxTokens > 0 ? String(model.maxTokens) : '',
      reasoningEffort: model.reasoningEffort || '',
    })),
    apiKey: '',
    isDefault: Boolean(profile.isDefault),
    stream: profile.stream !== false,
    active: profile.active !== false,
    supportsVision: Boolean(profile.supportsVision),
  };
}

function modelsPayload(input) {
  return (Array.isArray(input.models) ? input.models : [])
    .map((model) => ({
      model: String(model.model || '').trim(),
      label: String(model.label || '').trim(),
      max_tokens: parseMaxTokensInput(model.maxTokens),
      reasoning_effort: String(model.reasoningEffort || '').trim(),
    }))
    .filter((model) => model.model);
}

function parseMaxTokensInput(value) {
  if (value === '' || value == null) return 0;
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return 0;
  return Math.round(n);
}

export function providerProfileCreatePayload(input) {
  const models = modelsPayload(input);
  const defaultModel = models.some((item) => item.model === input.model)
    ? input.model : models[0]?.model || input.model;
  const defaultConfig = models.find((item) => item.model === defaultModel);
  return {
    name: input.name,
    provider: input.provider || 'openai_compatible',
    base_url: input.baseUrl,
    model: defaultModel,
    max_tokens: defaultConfig?.max_tokens ?? parseMaxTokensInput(input.maxTokens),
    models,
    api_key: input.apiKey,
    is_default: Boolean(input.isDefault),
    stream: input.stream !== false,
    supports_vision: Boolean(input.supportsVision),
  };
}

export function providerProfileUpdatePayload(input) {
  const models = modelsPayload(input);
  const defaultModel = models.some((item) => item.model === input.model)
    ? input.model : models[0]?.model || input.model;
  const defaultConfig = models.find((item) => item.model === defaultModel);
  return stripUndefined({
    name: input.name,
    provider: input.provider || 'openai_compatible',
    base_url: input.baseUrl,
    model: defaultModel,
    max_tokens: defaultConfig?.max_tokens ?? parseMaxTokensInput(input.maxTokens),
    models,
    api_key: input.apiKey || undefined,
    is_default: Boolean(input.isDefault),
    stream: input.stream !== false,
    active: input.active !== false,
    supports_vision: Boolean(input.supportsVision),
  });
}

export function providerModelFor(profile, modelName = '') {
  if (!profile) return null;
  const models = Array.isArray(profile.models) ? profile.models : [];
  return models.find((item) => item.model === modelName)
    || models.find((item) => item.model === profile.model)
    || models[0]
    || null;
}

function stripUndefined(value) {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined));
}
