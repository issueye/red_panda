import assert from 'node:assert/strict';
import test from 'node:test';
import {
  normalizeSkillDetail,
  normalizeSkillSummary,
  normalizeSkillsList,
  skillCreatePayload,
  skillUpdatePayload,
} from './skills.js';

test('normalizeSkillSummary maps gateway fields', () => {
  const item = normalizeSkillSummary({
    name: 'review',
    description: 'Review code',
    path: '.codex/skills/review/SKILL.md',
    has_instructions: true,
  });
  assert.equal(item.name, 'review');
  assert.equal(item.hasInstructions, true);
  assert.equal(item.path, '.codex/skills/review/SKILL.md');
});

test('normalizeSkillDetail accepts nested skill payload', () => {
  const item = normalizeSkillDetail({
    skill: {
      name: 'review',
      description: 'Review code',
      instructions: '# Steps',
      path: '.codex/skills/review/SKILL.md',
      size_bytes: 42,
    },
  });
  assert.equal(item.instructions, '# Steps');
  assert.equal(item.sizeBytes, 42);
});

test('skill payloads require workspace and trimmed fields', () => {
  assert.deepEqual(skillCreatePayload({
    name: ' review ',
    description: ' desc ',
    instructions: ' body ',
  }, 'E:/ws'), {
    workspace_root: 'E:/ws',
    name: 'review',
    description: 'desc',
    instructions: 'body',
  });
  assert.deepEqual(skillUpdatePayload({
    description: ' next ',
    instructions: ' next body ',
  }, 'E:/ws'), {
    workspace_root: 'E:/ws',
    description: 'next',
    instructions: 'next body',
  });
});

test('normalizeSkillsList accepts items envelope', () => {
  const items = normalizeSkillsList({
    items: [{ name: 'a', description: 'A', path: '.codex/skills/a/SKILL.md' }],
  });
  assert.equal(items.length, 1);
  assert.equal(items[0].name, 'a');
});
