import assert from 'node:assert/strict';
import test from 'node:test';

import {
  normalizeProviderProfile,
  profileDraftFrom,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
} from './providerProfiles.js';

test('normalizeProviderProfile maps masked key state and defaults', () => {
  const profile = normalizeProviderProfile({
    id: 'provider_1',
    name: '',
    base_url: 'https://provider.invalid',
    model: 'model-a',
    max_tokens: 128000,
    api_key_set: true,
    api_key_masked: '****1234',
    is_default: true,
  });

  assert.equal(profile.name, 'provider_1');
  assert.equal(profile.provider, 'openai_compatible');
  assert.equal(profile.baseUrl, 'https://provider.invalid');
  assert.equal(profile.model, 'model-a');
  assert.equal(profile.maxTokens, 128000);
  assert.equal(profile.apiKeySet, true);
  assert.equal(profile.apiKeyMasked, '****1234');
  assert.equal(profile.isDefault, true);
  assert.equal(profile.active, true);
});

test('profileDraftFrom never copies saved API key state into editable draft', () => {
  const draft = profileDraftFrom({
    name: 'Work',
    provider: 'openai_compatible',
    baseUrl: 'https://provider.invalid',
    model: 'model-a',
    maxTokens: 64000,
    apiKeySet: true,
    apiKeyMasked: '****1234',
    isDefault: true,
  });

  assert.equal(draft.name, 'Work');
  assert.equal(draft.apiKey, '');
  assert.equal(draft.maxTokens, '64000');
  assert.equal(draft.isDefault, true);
});

test('provider profile payloads use Gateway field names and omit blank update key', () => {
  assert.deepEqual(providerProfileCreatePayload({
    name: 'Work',
    provider: '',
    baseUrl: 'https://provider.invalid',
    model: 'model-a',
    maxTokens: '128000',
    apiKey: 'sk-live',
    isDefault: true,
  }), {
    name: 'Work',
    provider: 'openai_compatible',
    base_url: 'https://provider.invalid',
    model: 'model-a',
    max_tokens: 128000,
    api_key: 'sk-live',
    is_default: true,
  });

  const update = providerProfileUpdatePayload({
    name: 'Work',
    provider: 'openai_compatible',
    baseUrl: 'https://provider.invalid',
    model: 'model-b',
    maxTokens: '',
    apiKey: '',
    isDefault: false,
    active: true,
  });
  assert.equal(Object.hasOwn(update, 'api_key'), false);
  assert.equal(update.model, 'model-b');
  assert.equal(update.max_tokens, 0);
  assert.equal(update.active, true);
});
