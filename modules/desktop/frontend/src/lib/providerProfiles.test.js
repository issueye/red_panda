import assert from 'node:assert/strict';
import test from 'node:test';

import {
	baseUrlAfterProviderChange,
  normalizeProviderProfile,
  profileDraftFrom,
  providerProfileCreatePayload,
  providerProfileUpdatePayload,
} from './providerProfiles.js';

test('provider changes use official defaults without overwriting custom gateways', () => {
  assert.equal(baseUrlAfterProviderChange('', 'anthropic'), 'https://api.anthropic.com');
  assert.equal(baseUrlAfterProviderChange('https://api.openai.com', 'openai_responses'), 'https://api.openai.com');
  assert.equal(baseUrlAfterProviderChange('https://gateway.example/v1', 'anthropic'), 'https://gateway.example/v1');
});

test('normalizeProviderProfile maps masked key state and defaults', () => {
  const profile = normalizeProviderProfile({
    id: 'provider_1',
    name: '',
    base_url: 'https://provider.invalid',
    model: 'model-a',
    max_tokens: 128000,
    models: [
      { model: 'model-a', label: 'Fast', max_tokens: 128000, reasoning_effort: 'medium' },
      { model: 'model-b', max_tokens: 200000, reasoning_effort: 'high' },
    ],
    api_key_set: true,
    api_key_masked: '****1234',
    is_default: true,
  });

  assert.equal(profile.name, 'provider_1');
  assert.equal(profile.provider, 'openai_compatible');
  assert.equal(profile.baseUrl, 'https://provider.invalid');
  assert.equal(profile.model, 'model-a');
  assert.equal(profile.maxTokens, 128000);
  assert.deepEqual(profile.models, [
    { model: 'model-a', label: 'Fast', maxTokens: 128000, reasoningEffort: 'medium' },
    { model: 'model-b', label: '', maxTokens: 200000, reasoningEffort: 'high' },
  ]);
  assert.equal(profile.apiKeySet, true);
  assert.equal(profile.apiKeyMasked, '****1234');
  assert.equal(profile.isDefault, true);
  assert.equal(profile.stream, true);
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
  assert.deepEqual(draft.models, [{ model: 'model-a', label: '', maxTokens: '64000', reasoningEffort: '' }]);
  assert.equal(draft.isDefault, true);
  assert.equal(draft.stream, true);
});

test('provider profile payloads use Gateway field names and omit blank update key', () => {
  assert.deepEqual(providerProfileCreatePayload({
    name: 'Work',
    provider: '',
    baseUrl: 'https://provider.invalid',
    model: 'model-a',
    maxTokens: '128000',
    models: [
      { model: 'model-a', label: 'Fast', maxTokens: '128000', reasoningEffort: 'medium' },
      { model: 'model-b', label: '', maxTokens: '200000', reasoningEffort: 'high' },
    ],
    apiKey: 'sk-live',
    isDefault: true,
    stream: false,
    httpProxy: '  http://127.0.0.1:7890  ',
  }), {
    name: 'Work',
    provider: 'openai_compatible',
    base_url: 'https://provider.invalid',
    model: 'model-a',
    max_tokens: 128000,
    models: [
      { model: 'model-a', label: 'Fast', max_tokens: 128000, reasoning_effort: 'medium' },
      { model: 'model-b', label: '', max_tokens: 200000, reasoning_effort: 'high' },
    ],
    api_key: 'sk-live',
    is_default: true,
    stream: false,
    supports_vision: false,
    http_proxy: 'http://127.0.0.1:7890',
  });

  const update = providerProfileUpdatePayload({
    name: 'Work',
    provider: 'openai_compatible',
    baseUrl: 'https://provider.invalid',
    model: 'model-b',
    maxTokens: '',
    models: [{ model: 'model-b', label: '', maxTokens: '', reasoningEffort: 'high' }],
    apiKey: '',
    isDefault: false,
    stream: true,
    active: true,
    supportsVision: true,
    httpProxy: 'socks5://127.0.0.1:1080',
  });
  assert.equal(Object.hasOwn(update, 'api_key'), false);
  assert.equal(update.model, 'model-b');
  assert.equal(update.max_tokens, 0);
  assert.deepEqual(update.models, [{ model: 'model-b', label: '', max_tokens: 0, reasoning_effort: 'high' }]);
  assert.equal(update.active, true);
  assert.equal(update.stream, true);
  assert.equal(update.supports_vision, true);
  assert.equal(update.http_proxy, 'socks5://127.0.0.1:1080');

  const anthropic = providerProfileCreatePayload({
    name: 'Claude',
    provider: 'anthropic',
    baseUrl: 'https://api.anthropic.com',
    model: 'claude-sonnet-4-5',
    maxTokens: '',
    models: [{ model: 'claude-sonnet-4-5', label: '', maxTokens: '', reasoningEffort: 'high' }],
    apiKey: 'secret',
    isDefault: false,
  });
  assert.equal(anthropic.provider, 'anthropic');
});
