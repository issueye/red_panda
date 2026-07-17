import test from 'node:test';
import assert from 'node:assert/strict';
import {
  normalizeSchedule,
  scheduleCreatePayload,
  scheduleSummary,
} from './schedules.js';

test('normalizeSchedule maps snake_case fields', () => {
  const item = normalizeSchedule({
    id: 'sched_1',
    name: 'n',
    schedule_kind: 'cron',
    cron_expr: '0 9 * * *',
    enabled: true,
    next_run_at: '2026-07-16T01:00:00Z',
  });
  assert.equal(item.id, 'sched_1');
  assert.equal(item.scheduleKind, 'cron');
  assert.equal(item.cronExpr, '0 9 * * *');
  assert.equal(item.nextRunAt, '2026-07-16T01:00:00Z');
});

test('scheduleCreatePayload builds interval request', () => {
  const payload = scheduleCreatePayload(
    {
      name: ' dig ',
      scheduleKind: 'interval',
      intervalSec: 120,
      prompt: 'hello',
      workspaceRoot: 'E:/ws',
    },
    {},
  );
  assert.equal(payload.name, 'dig');
  assert.equal(payload.schedule_kind, 'interval');
  assert.equal(payload.interval_sec, 120);
  assert.equal(payload.permission_mode, 'deny');
  assert.equal(payload.tool_policy, 'allowlist');
  assert.equal(payload.workspace_root, 'E:/ws');
});

test('scheduleSummary formats interval hours', () => {
  assert.equal(scheduleSummary({ scheduleKind: 'interval', intervalSec: 3600 }), '每 1 小时');
  assert.equal(scheduleSummary({ scheduleKind: 'cron', cronExpr: '0 9 * * *' }), 'cron 0 9 * * *');
});
