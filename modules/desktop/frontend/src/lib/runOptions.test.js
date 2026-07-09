import assert from 'node:assert/strict';
import test from 'node:test';

import { buildRunStartOptions, splitOptionList } from './runOptions.js';

test('splitOptionList trims empty entries', () => {
  assert.deepEqual(
    splitOptionList(' workspace.read_file, ,shell.exec '),
    ['workspace.read_file', 'shell.exec'],
  );
});

test('buildRunStartOptions includes provider profile and tool policy fields', () => {
  const options = buildRunStartOptions({
    runtimeMode: 'per_run_process',
    toolPolicy: 'ask_all',
    permissionMode: 'permissive',
    providerProfileId: 'provider_1',
    model: ' model-a ',
    toolAllowlist: 'workspace.read_file, workspace.list',
    toolDenylist: 'shell.exec',
    spawnSubAgents: false,
    subAgentBackend: 'process_pool',
  }, { root_path: 'D:/workspace' }, 'do work /permission /subagent');

  assert.equal(options.working_dir, 'D:/workspace');
  assert.equal(options.runtime_mode, 'per_run_process');
  assert.equal(options.tool_policy, 'ask_all');
  assert.equal(options.permission_mode, 'permissive');
  assert.equal(options.provider_profile_id, 'provider_1');
  assert.equal(options.model, 'model-a');
  assert.deepEqual(options.tool_allowlist, ['workspace.read_file', 'workspace.list']);
  assert.deepEqual(options.tool_denylist, ['shell.exec']);
  assert.equal(options.require_permission, true);
  assert.equal(options.spawn_subagents, true);
  assert.equal(options.subagent_backend, 'process_pool');
});
