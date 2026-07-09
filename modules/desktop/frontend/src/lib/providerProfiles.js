export const emptyProfileDraft = {
  name: '',
  provider: 'openai_compatible',
  baseUrl: '',
  model: '',
  apiKey: '',
  isDefault: false,
  active: true,
};

export function normalizeProviderProfile(item) {
  return {
    id: item.id,
    name: item.name || item.id,
    provider: item.provider || 'openai_compatible',
    baseUrl: item.base_url || '',
    model: item.model || '',
    apiKeySet: Boolean(item.api_key_set),
    apiKeyMasked: item.api_key_masked || item.masked_api_key || item.api_key_preview || '',
    isDefault: Boolean(item.is_default),
    active: item.active !== false,
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
    apiKey: '',
    isDefault: Boolean(profile.isDefault),
    active: profile.active !== false,
  };
}

export function providerProfileCreatePayload(input) {
  return {
    name: input.name,
    provider: input.provider || 'openai_compatible',
    base_url: input.baseUrl,
    model: input.model,
    api_key: input.apiKey,
    is_default: Boolean(input.isDefault),
  };
}

export function providerProfileUpdatePayload(input) {
  return stripUndefined({
    name: input.name,
    provider: input.provider || 'openai_compatible',
    base_url: input.baseUrl,
    model: input.model,
    api_key: input.apiKey || undefined,
    is_default: Boolean(input.isDefault),
    active: input.active !== false,
  });
}

function stripUndefined(value) {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined));
}
