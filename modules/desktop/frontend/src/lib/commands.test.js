import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildStartGoalInput,
  listCommands,
  looksLikeCommand,
  parseCommand,
} from './commands.js';

test('looksLikeCommand detects leading slash', () => {
  assert.equal(looksLikeCommand('/goal fix login'), true);
  assert.equal(looksLikeCommand('  /help'), true);
  assert.equal(looksLikeCommand('please /permission'), false);
  assert.equal(looksLikeCommand('plain text'), false);
});

test('parseCommand plain run keeps text and permission flag', () => {
  const cmd = parseCommand('do work /permission');
  assert.equal(cmd.action, 'run');
  assert.equal(cmd.isCommand, false);
  assert.equal(cmd.inputText, 'do work /permission');
  assert.equal(cmd.requirePermission, true);
  assert.equal(cmd.spawnSubAgents, undefined);
});

test('parseCommand /goal starts a user-initiated goal', () => {
  const cmd = parseCommand('/goal 实现用户登录');
  assert.equal(cmd.action, 'start_goal');
  assert.equal(cmd.isCommand, true);
  assert.equal(cmd.objective, '实现用户登录');
  assert.equal(cmd.displayText, '目标：实现用户登录');
  assert.match(cmd.inputText, /\[启动目标\]/);
  assert.match(cmd.inputText, /实现用户登录/);
  assert.ok(cmd.successCriteria);
});

test('parseCommand /goal continue and cancel', () => {
  const cont = parseCommand('/goal continue 优先修测试');
  assert.equal(cont.action, 'continue_goal');
  assert.equal(cont.extraText, '优先修测试');
  assert.equal(cont.displayText, '继续目标：优先修测试');

  const cancel = parseCommand('/goal cancel');
  assert.equal(cancel.action, 'cancel_goal');
  assert.equal(cancel.displayText, '取消目标');
});

test('parseCommand /goal without args shows help', () => {
  const cmd = parseCommand('/goal');
  assert.equal(cmd.action, 'help');
  assert.match(cmd.message, /Goal 指令/);
});

test('parseCommand /help', () => {
  const cmd = parseCommand('/help');
  assert.equal(cmd.action, 'help');
  assert.match(cmd.message, /\/goal/);
});

test('parseCommand unknown command errors', () => {
  const cmd = parseCommand('/unknown-thing');
  assert.equal(cmd.action, 'error');
  assert.match(cmd.message, /未知指令/);
});

test('parseCommand aliases', () => {
  assert.equal(parseCommand('/g 任务 A').action, 'start_goal');
  assert.equal(parseCommand('/目标 任务 B').action, 'start_goal');
  assert.equal(parseCommand('/goal c 补充').action, 'continue_goal');
  assert.equal(parseCommand('/goal x').action, 'cancel_goal');
});

test('buildStartGoalInput includes binding instructions', () => {
  const text = buildStartGoalInput({
    title: '登录',
    objective: '实现登录',
    successCriteria: '能登录',
  });
  assert.match(text, /已由用户创建/);
  assert.match(text, /goal\.observe/);
  assert.match(text, /不要创建第二个 Goal/);
});

test('listCommands exposes goal entries', () => {
  const names = listCommands().map((c) => c.name);
  assert.ok(names.includes('goal'));
  assert.ok(names.includes('help'));
});
