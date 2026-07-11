import assert from 'node:assert/strict';
import test from 'node:test';
import { buildWorkspaceSessionTree, normalizeWorkspaceRoot, workspaceDisplayName } from './sessionTree.js';

test('normalizeWorkspaceRoot unifies separators and trailing slash', () => {
  assert.equal(normalizeWorkspaceRoot('E:\\code\\demo\\'), 'e:/code/demo');
  assert.equal(normalizeWorkspaceRoot('E:/code/demo'), 'e:/code/demo');
});

test('workspaceDisplayName returns last path segment', () => {
  assert.equal(workspaceDisplayName('E:/code/red_panda'), 'red_panda');
  assert.equal(workspaceDisplayName(''), '未绑定工作区');
});

test('buildWorkspaceSessionTree groups sessions under workspace nodes', () => {
  const tree = buildWorkspaceSessionTree(
    [
      { id: 's1', title: 'A', workspaceRoot: 'E:/ws/alpha' },
      { id: 's2', title: 'B', workspaceRoot: 'E:\\ws\\alpha\\' },
      { id: 's3', title: 'C', workspaceRoot: 'E:/ws/beta' },
      { id: 's4', title: 'D', workspaceRoot: '' },
    ],
    [
      { id: 'ws1', root: 'E:/ws/alpha', name: 'Alpha' },
      { id: 'ws2', root: 'E:/ws/beta', name: 'Beta' },
    ],
    { id: 'ws1', root: 'E:/ws/alpha', name: 'Alpha' },
  );

  assert.equal(tree.length, 3);
  assert.equal(tree[0].name, 'Alpha');
  assert.equal(tree[0].isCurrent, true);
  assert.equal(tree[0].sessions.length, 2);
  assert.equal(tree.find((item) => item.name === 'Beta')?.sessions[0].id, 's3');
  assert.equal(tree.find((item) => !item.root)?.sessions[0].id, 's4');
});
