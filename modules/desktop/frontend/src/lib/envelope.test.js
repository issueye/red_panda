import assert from 'node:assert/strict';
import { test } from 'node:test';
import { listFromEnvelope, mapListFromEnvelope } from './envelope.js';

test('listFromEnvelope accepts bare arrays and common keys', () => {
  assert.deepEqual(listFromEnvelope([{ id: '1' }]), [{ id: '1' }]);
  assert.deepEqual(listFromEnvelope({ items: [{ id: 'a' }] }), [{ id: 'a' }]);
  assert.deepEqual(listFromEnvelope({ data: [{ id: 'd' }] }), [{ id: 'd' }]);
  assert.deepEqual(listFromEnvelope({ notes: [{ id: 'n' }] }, { keys: ['notes', 'items'] }), [{ id: 'n' }]);
  assert.deepEqual(listFromEnvelope(null), []);
  assert.deepEqual(listFromEnvelope({}), []);
});

test('mapListFromEnvelope maps items', () => {
  const out = mapListFromEnvelope({ items: [{ id: '1' }, { id: '2' }] }, (item) => item.id);
  assert.deepEqual(out, ['1', '2']);
});
