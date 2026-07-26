import assert from 'node:assert/strict';
import test from 'node:test';

import {
  listCommands,
  looksLikeCommand,
  parseCommand,
} from './commands.js';

test('looksLikeCommand detects leading slash', () => {
  assert.equal(looksLikeCommand('/help'), true);
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

test('parseCommand /help', () => {
  const cmd = parseCommand('/help');
  assert.equal(cmd.action, 'help');
  assert.match(cmd.message, /\/help/);
});

test('parseCommand unknown command errors', () => {
  const cmd = parseCommand('/unknown-thing');
  assert.equal(cmd.action, 'error');
  assert.match(cmd.message, /未知指令/);
});

test('parseCommand /goal now errors as unknown command', () => {
  const cmd = parseCommand('/goal 实现用户登录');
  assert.equal(cmd.action, 'error');
  assert.match(cmd.message, /未知指令/);
});

test('listCommands exposes help and permission entries', () => {
  const names = listCommands().map((c) => c.name);
  assert.ok(names.includes('help'));
  assert.ok(names.includes('permission'));
  assert.ok(!names.includes('goal'));
});
