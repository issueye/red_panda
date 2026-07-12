import assert from 'node:assert/strict';
import test from 'node:test';
import {
  appendDiagnosticLog,
  clearDiagnosticLogs,
  getDiagnosticLogs,
  subscribeDiagnosticLogs,
} from './diagnosticLog.js';

test('appendDiagnosticLog stores and notifies subscribers', () => {
  clearDiagnosticLogs();
  let seen = [];
  const unsub = subscribeDiagnosticLogs((items) => {
    seen = items;
  });
  appendDiagnosticLog('info', 'hello', { source: 'test', detail: { a: 1 } });
  assert.equal(getDiagnosticLogs().length, 1);
  assert.equal(seen.length, 1);
  assert.equal(seen[0].message, 'hello');
  assert.match(seen[0].detail, /"a": 1/);
  clearDiagnosticLogs();
  assert.equal(getDiagnosticLogs().length, 0);
  unsub();
});
