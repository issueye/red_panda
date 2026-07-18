import assert from 'node:assert/strict';
import test from 'node:test';

import { resolveWorkspaceFileHref } from './workspaceLinks.js';

test('resolveWorkspaceFileHref resolves relative Markdown links in Windows workspaces', () => {
  assert.equal(
    resolveWorkspaceFileHref('docs/plugin-ipc-sdk.md', 'E:\\codes\\project'),
    'E:\\codes\\project\\docs\\plugin-ipc-sdk.md',
  );
  assert.equal(
    resolveWorkspaceFileHref('./plugins/grokbuild/README.md#usage', 'E:\\codes\\project\\'),
    'E:\\codes\\project\\plugins\\grokbuild\\README.md',
  );
});

test('resolveWorkspaceFileHref rejects external and escaping links', () => {
  assert.equal(resolveWorkspaceFileHref('https://example.com/file.md', 'E:\\codes\\project'), '');
  assert.equal(resolveWorkspaceFileHref('../../outside.txt', 'E:\\codes\\project'), '');
  assert.equal(resolveWorkspaceFileHref('#section', 'E:\\codes\\project'), '');
});

