import assert from 'node:assert/strict';
import test from 'node:test';
import { buildGatewayChildEnv } from './gatewayProcess.js';

test('buildGatewayChildEnv does not force RED_PANDA_SLASH_TOOLS (docs/36 C5)', () => {
  const env = buildGatewayChildEnv(
    {
      PATH: 'C:\\bin',
      // Parent may carry unrelated vars; helper must not inject slash debug on.
    },
    {},
  );
  assert.equal(env.RED_PANDA_GATEWAY_ADDR, '127.0.0.1:17931');
  assert.ok(env.RED_PANDA_DATABASE);
  assert.ok(env.RED_PANDA_AGENT_COMMAND);
  assert.equal(
    Object.prototype.hasOwnProperty.call(env, 'RED_PANDA_SLASH_TOOLS'),
    false,
    'default e2e env must not enable Runtime slash Parse',
  );
});

test('buildGatewayChildEnv allows explicit opt-in via extraEnv only', () => {
  const env = buildGatewayChildEnv({ PATH: 'x' }, { RED_PANDA_SLASH_TOOLS: '1' });
  assert.equal(env.RED_PANDA_SLASH_TOOLS, '1');
});

test('buildGatewayChildEnv preserves parent SLASH_TOOLS when already set', () => {
  // Inherit only if the outer process intentionally set it; we still do not force it.
  const env = buildGatewayChildEnv({ RED_PANDA_SLASH_TOOLS: 'true', PATH: 'x' }, {});
  assert.equal(env.RED_PANDA_SLASH_TOOLS, 'true');
});
