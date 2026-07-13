import assert from 'node:assert/strict';
import test from 'node:test';

import { buildRunStartOptions, defaultRunSettings, splitOptionList } from './runOptions.js';

test('default subagent backend prefers runtime_process over process_pool', () => {
  assert.equal(defaultRunSettings.subAgentBackend, 'runtime_process');
  const options = buildRunStartOptions({}, { root_path: 'D:/ws' }, 'hello');
  assert.equal(options.subagent_backend, 'runtime_process');
});

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

test('buildRunStartOptions passes web tool tuning fields', () => {
  const options = buildRunStartOptions({
    webSearchResults: '5',
    webFetchMaxBytes: '1048576',
    webSearchProvider: 'tavily',
    webTavilyApiKey: ' tvly-test ',
    webHttpProxy: ' http://127.0.0.1:7890 ',
  }, { root_path: 'D:/ws' }, 'search the web');

  assert.equal(options.web_search_max_results, 5);
  assert.equal(options.web_fetch_max_bytes, 1048576);
  assert.equal(options.web_search_provider, 'tavily');
  assert.equal(options.web_tavily_api_key, 'tvly-test');
  assert.equal(options.web_http_proxy, 'http://127.0.0.1:7890');
});

test('buildRunStartOptions falls back to default web tuning', () => {
  const options = buildRunStartOptions({}, { root: 'D:/ws' }, 'plain text');
  assert.equal(options.web_search_max_results, 8);
  assert.equal(options.web_fetch_max_bytes, 2097152);
  assert.equal(options.web_search_provider, 'auto');
  assert.equal(options.web_tavily_api_key, '');
  assert.equal(options.web_http_proxy, '');
  assert.equal(options.max_tool_turns, 12);
  assert.equal(options.runtime_mode, 'per_run_process');
  assert.equal(options.max_concurrent_runs, 3);
  assert.equal(options.log_llm_requests, false);
  assert.equal(options.goals_enabled, false);
});

test('buildRunStartOptions keeps regular conversations out of the goal pipeline', () => {
  const options = buildRunStartOptions({}, { root: 'D:/ws' }, 'analyze this project');

  assert.equal(options.create_goal, undefined);
  assert.equal(options.goal_id, undefined);
  assert.equal(options.continue_goal, undefined);
  assert.equal(options.goals_enabled, false);
});

test('buildRunStartOptions passes log_llm_requests toggle', () => {
  const options = buildRunStartOptions({
    logLlmRequests: true,
  }, { root: 'D:/ws' }, 'debug llm');
  assert.equal(options.log_llm_requests, true);
});

test('buildRunStartOptions passes max tool turns', () => {
  const options = buildRunStartOptions({
    maxToolTurns: '12',
  }, { root: 'D:/ws' }, 'use many tools');
  assert.equal(options.max_tool_turns, 12);
});

test('buildRunStartOptions passes max concurrent runs', () => {
  const options = buildRunStartOptions({
    maxConcurrentRuns: '5',
  }, { root: 'D:/ws' }, 'parallel sessions');
  assert.equal(options.max_concurrent_runs, 5);
});

test('buildRunStartOptions accepts create_goal overrides from command system', () => {
  const options = buildRunStartOptions(
    { spawnSubAgents: false },
    { root: 'D:/ws' },
    '/goal 实现登录',
    {
      create_goal: true,
      goal_objective: '实现登录',
      goal_title: '实现登录',
      goal_success_criteria: '能登录',
      require_permission: false,
      spawn_subagents: true,
    },
  );
  assert.equal(options.create_goal, true);
  assert.equal(options.goal_objective, '实现登录');
  assert.equal(options.goal_title, '实现登录');
  assert.equal(options.goal_success_criteria, '能登录');
  assert.equal(options.goals_enabled, true);
  assert.equal(options.spawn_subagents, true);
  assert.equal(options.require_permission, false);
});
