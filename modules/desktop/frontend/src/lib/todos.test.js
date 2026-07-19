import assert from 'node:assert/strict';
import test from 'node:test';
import {
  countOpenTodos,
  normalizeTodo,
  todosFromToolFinishedPayload,
  todosFromUpdatedEvent,
  todoStatusLabel,
} from './todos.js';

test('normalizeTodo maps snake_case fields', () => {
  const item = normalizeTodo({
    id: 'todo_1',
    client_key: '1',
    content: '实现任务条',
    status: 'in_progress',
    sort_order: 2,
    active_form: '正在实现',
  });
  assert.equal(item.clientKey, '1');
  assert.equal(item.sortOrder, 2);
  assert.equal(item.activeForm, '正在实现');
});

test('countOpenTodos counts pending and in_progress', () => {
  assert.equal(countOpenTodos([
    { status: 'pending' },
    { status: 'in_progress' },
    { status: 'completed' },
    { status: 'cancelled' },
  ]), 2);
});

test('todosFromUpdatedEvent', () => {
  const result = todosFromUpdatedEvent({
    items: [{ id: 'a', content: 'x', status: 'pending' }],
    open_count: 1,
  });
  assert.equal(result.openCount, 1);
  assert.equal(result.items[0].content, 'x');
});

test('todosFromToolFinishedPayload parses standard envelope', () => {
  const output = JSON.stringify({
    schema: 'red_panda.tool_result.v1',
    tool: 'todo.write',
    status: 'completed',
    ok: true,
    data: {
      items: [{ id: 'todo_1', client_key: '1', content: 'a', status: 'pending' }],
      open_count: 1,
    },
  });
  const result = todosFromToolFinishedPayload({ tool_name: 'todo.write', output });
  const legacy = todosFromToolFinishedPayload({ tool_name: 'todo_write', output });
  assert.ok(legacy);
  assert.equal(legacy.items.length, result.items.length);
  assert.ok(result);
  assert.equal(result.items[0].clientKey, '1');
  assert.equal(result.openCount, 1);
});

test('todoStatusLabel chinese', () => {
  assert.equal(todoStatusLabel('in_progress'), '进行中');
  assert.equal(todoStatusLabel('completed'), '已完成');
});
