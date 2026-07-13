import assert from 'node:assert/strict';
import test from 'node:test';

import { gatewayBase } from './api.js';

test('gatewayBase is a non-empty string for local HTTP', () => {
  assert.equal(typeof gatewayBase, 'string');
  assert.ok(gatewayBase.length > 0);
});
