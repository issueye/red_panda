import assert from 'node:assert/strict';
import test from 'node:test';
import {
  goalNoteHeadline,
  goalNoteKindLabel,
  normalizeGoalNote,
  normalizeGoalNoteList,
} from './goalNotes.js';

test('normalizeGoalNote maps Gateway DTO fields', () => {
  const note = normalizeGoalNote({
    id: 'note_1',
    goal_id: 'g1',
    kind: 'Finding',
    title: 'T',
    body: 'body',
    phase: 'analyze',
    source: 'root',
    run_id: 'r1',
    seq: 3,
    pinned: 1,
    created_at: '2026-01-01T00:00:00Z',
  });
  assert.equal(note.goalId, 'g1');
  assert.equal(note.kind, 'finding');
  assert.equal(note.pinned, true);
  assert.equal(note.runId, 'r1');
  assert.equal(note.seq, 3);
});

test('normalizeGoalNoteList accepts items envelope', () => {
  const list = normalizeGoalNoteList({
    items: [{ id: 'a', kind: 'decision', title: 'Use SQLite', body: 'yes' }],
    count: 1,
  });
  assert.equal(list.length, 1);
  assert.equal(list[0].kind, 'decision');
  assert.equal(list[0].title, 'Use SQLite');
});

test('goalNoteKindLabel and headline', () => {
  assert.equal(goalNoteKindLabel('risk'), '风险');
  assert.equal(goalNoteHeadline({ title: 'Pinned' }), 'Pinned');
  assert.equal(
    goalNoteHeadline({ body: 'a'.repeat(100) }).endsWith('…'),
    true,
  );
});
