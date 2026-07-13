import assert from 'node:assert/strict';
import test from 'node:test';
import {
  autoContinueDedupeKey,
  cancelSessionGoal,
  continueSessionGoal,
  hydrateSessionGoals,
  hydrateSessionTodos,
  shouldAttemptAutoContinue,
} from './useGoalSession.js';

function makePatchCollector() {
  const patches = [];
  const state = {
    todos: [],
    todoOpenCount: 0,
    todosHydrated: false,
    todosVersion: 0,
    goals: [],
    goal: null,
    goalHydrated: false,
    goalExpanded: false,
    goalBusy: false,
    running: false,
    currentRunId: '',
    draft: 'keep-me',
  };
  const patchRuntime = (sessionId, updater) => {
    const next = updater(state);
    Object.assign(state, next);
    patches.push({ sessionId, next: { ...state } });
  };
  return { state, patches, patchRuntime };
}

test('hydrateSessionTodos applies Gateway list + open_count', async () => {
  const { state, patches, patchRuntime } = makePatchCollector();
  const fetchJson = async (path) => {
    assert.match(path, /\/api\/v1\/sessions\/s1\/todos$/);
    return {
      items: [
        { id: 't1', content: 'a', status: 'pending', client_key: '1', sort_order: 0 },
        { id: 't2', content: 'b', status: 'completed', client_key: '2', sort_order: 1 },
      ],
      open_count: 1,
    };
  };
  await hydrateSessionTodos('s1', patchRuntime, fetchJson);
  assert.equal(state.todosHydrated, true);
  assert.equal(state.todoOpenCount, 1);
  assert.equal(state.todos.length, 2);
  assert.equal(state.todos[0].content, 'a');
  assert.equal(state.todosVersion, 1);
  assert.equal(patches[0].sessionId, 's1');
});

test('hydrateSessionTodos marks hydrated on fetch error', async () => {
  const { state, patchRuntime } = makePatchCollector();
  await hydrateSessionTodos('s1', patchRuntime, async () => {
    throw new Error('network');
  });
  assert.equal(state.todosHydrated, true);
  assert.equal(state.todos.length, 0);
});

test('hydrateSessionTodos no-ops without sessionId', async () => {
  let called = false;
  await hydrateSessionTodos('', () => { called = true; }, async () => ({}));
  assert.equal(called, false);
});

test('hydrateSessionGoals projects focus goal from Gateway list', async () => {
  const { state, patchRuntime } = makePatchCollector();
  const fetchJson = async (path) => {
    assert.match(path, /\/api\/v1\/sessions\/s1\/goals$/);
    return {
      items: [
        {
          id: 'g-paused',
          status: 'paused',
          objective: 'paused obj',
          pause_reason: 'awaiting_continue',
          used_tool_turns: 2,
          max_total_tool_turns: 10,
        },
        {
          id: 'g-active',
          status: 'active',
          objective: 'active obj',
          used_tool_turns: 1,
          max_total_tool_turns: 10,
        },
      ],
    };
  };
  await hydrateSessionGoals('s1', patchRuntime, fetchJson);
  assert.equal(state.goalHydrated, true);
  assert.equal(state.goals.length, 2);
  assert.equal(state.goal.id, 'g-active');
  assert.equal(state.goal.status, 'active');
});

test('hydrateSessionGoals skips local-design and missing session', async () => {
  let called = false;
  const patch = () => { called = true; };
  await hydrateSessionGoals('local-design', patch, async () => ({ items: [] }));
  await hydrateSessionGoals('', patch, async () => ({ items: [] }));
  assert.equal(called, false);
});

test('hydrateSessionGoals marks hydrated on fetch error', async () => {
  const { state, patchRuntime } = makePatchCollector();
  await hydrateSessionGoals('s1', patchRuntime, async () => {
    throw new Error('boom');
  });
  assert.equal(state.goalHydrated, true);
  assert.equal(state.goal, null);
});

test('continueSessionGoal posts continue and sets running', async () => {
  const { state, patchRuntime } = makePatchCollector();
  state.goal = { id: 'g1', status: 'paused' };
  const calls = [];
  const fetchJson = async (path, options = {}) => {
    calls.push({ path, options });
    return { run_id: 'run-9' };
  };
  const hydrated = [];
  const result = await continueSessionGoal({
    sessionId: 's1',
    goal: state.goal,
    extraText: 'go on',
    optionsOverrides: { goals_enabled: true },
    runSettings: { providerProfileId: 'p1', runtimeMode: 'agent' },
    workspace: { root_path: 'E:/ws' },
    patchRuntime,
    hydrateGoals: async (id) => { hydrated.push(id); },
    fetchJson,
  });
  assert.equal(result.run_id, 'run-9');
  assert.equal(state.running, true);
  assert.equal(state.currentRunId, 'run-9');
  assert.equal(state.goalBusy, false);
  assert.equal(state.draft, '');
  assert.deepEqual(hydrated, ['s1']);
  assert.match(calls[0].path, /\/goals\/g1\/continue$/);
  assert.equal(calls[0].options.method, 'POST');
  const body = JSON.parse(calls[0].options.body);
  assert.equal(body.input, 'go on');
  assert.equal(body.options.goals_enabled, true);
});

test('continueSessionGoal throws without goal and clears busy on API error', async () => {
  await assert.rejects(
    () => continueSessionGoal({
      sessionId: 's1',
      goal: null,
      runSettings: {},
      workspace: null,
      patchRuntime: () => {},
      hydrateGoals: async () => {},
    }),
    /没有可继续/,
  );

  const { state, patchRuntime } = makePatchCollector();
  await assert.rejects(
    () => continueSessionGoal({
      sessionId: 's1',
      goal: { id: 'g1' },
      runSettings: {},
      workspace: null,
      patchRuntime,
      hydrateGoals: async () => {},
      fetchJson: async () => { throw new Error('denied'); },
    }),
    /denied/,
  );
  assert.equal(state.goalBusy, false);
  assert.equal(state.running, false);
});

test('cancelSessionGoal cancels run then goal then rehydrates', async () => {
  const { state, patchRuntime } = makePatchCollector();
  state.goal = { id: 'g1', status: 'active' };
  state.currentRunId = 'run-1';
  state.running = true;
  const rpc = [];
  const http = [];
  const hydrated = [];
  await cancelSessionGoal({
    sessionId: 's1',
    goal: state.goal,
    runId: 'run-1',
    request: async (method, params) => {
      rpc.push({ method, params });
      return {};
    },
    patchRuntime,
    hydrateGoals: async (id) => { hydrated.push(id); },
    fetchJson: async (path, options = {}) => {
      http.push({ path, options });
      return {};
    },
  });
  assert.equal(rpc[0].method, 'run.cancel');
  assert.equal(rpc[0].params.run_id, 'run-1');
  assert.match(http[0].path, /\/goals\/g1\/cancel$/);
  assert.equal(http[0].options.method, 'POST');
  assert.deepEqual(hydrated, ['s1']);
  assert.equal(state.goalBusy, false);
  assert.equal(state.running, false);
  assert.equal(state.currentRunId, '');
});

test('autoContinueDedupeKey and shouldAttemptAutoContinue policy gates', () => {
  const goal = {
    id: 'g1',
    status: 'paused',
    pauseReason: 'awaiting_continue',
    usedToolTurns: 3,
    maxTotalToolTurns: 10,
    updatedAt: 't1',
  };
  assert.equal(autoContinueDedupeKey('s1', goal), 's1:g1:t1');
  assert.equal(
    shouldAttemptAutoContinue({
      sessionId: 's1',
      goal,
      running: false,
      compacting: false,
      goalBusy: false,
    }),
    true,
  );
  assert.equal(
    shouldAttemptAutoContinue({
      sessionId: 's1',
      goal,
      running: true,
      compacting: false,
      goalBusy: false,
    }),
    false,
  );
  assert.equal(
    shouldAttemptAutoContinue({
      sessionId: 'local-design',
      goal,
      running: false,
      compacting: false,
      goalBusy: false,
    }),
    false,
  );
  assert.equal(
    shouldAttemptAutoContinue({
      sessionId: 's1',
      goal: { ...goal, pauseReason: 'user_cancel' },
      running: false,
      compacting: false,
      goalBusy: false,
    }),
    false,
  );
});
