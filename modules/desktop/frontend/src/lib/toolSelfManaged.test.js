import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { isSelfManagedTool, shouldMarkToolStale } from './toolSelfManaged.js';

describe('toolSelfManaged', () => {
  it('recognizes worker.* tools as self-managed', () => {
    assert.equal(isSelfManagedTool('worker.delegate'), true);
    assert.equal(isSelfManagedTool('worker.cancel'), true);
    assert.equal(isSelfManagedTool('worker.send'), true);
    assert.equal(isSelfManagedTool('worker.receive'), true);
  });

  it('recognizes other self-managed tools', () => {
    assert.equal(isSelfManagedTool('shell.exec'), true);
    assert.equal(isSelfManagedTool('web.search'), true);
    assert.equal(isSelfManagedTool('web.fetch'), true);
    assert.equal(isSelfManagedTool('skill.run'), true);
  });

  it('normalizes double-underscore names', () => {
    assert.equal(isSelfManagedTool('worker__delegate'), true);
    assert.equal(isSelfManagedTool('shell__exec'), true);
  });

  it('returns false for ordinary tools', () => {
    assert.equal(isSelfManagedTool('workspace.read_file'), false);
    assert.equal(isSelfManagedTool('memory.create'), false);
    assert.equal(isSelfManagedTool('todo.write'), false);
    assert.equal(isSelfManagedTool(''), false);
    assert.equal(isSelfManagedTool(null), false);
    assert.equal(isSelfManagedTool(undefined), false);
  });

  it('shouldMarkToolStale returns false for self-managed tools even after long time', () => {
    assert.equal(shouldMarkToolStale('worker.delegate', 30000), false);
    assert.equal(shouldMarkToolStale('worker.cancel', 30000), false);
    assert.equal(shouldMarkToolStale('worker.send', 120000), false);
    assert.equal(shouldMarkToolStale('shell.exec', 45000), false);
  });

  it('shouldMarkToolStale returns true for normal tools after threshold', () => {
    assert.equal(shouldMarkToolStale('workspace.read_file', 25000), true);
    assert.equal(shouldMarkToolStale('memory.create', 30000), true);
  });

  it('shouldMarkToolStale returns false before threshold even for normal tools', () => {
    assert.equal(shouldMarkToolStale('workspace.read_file', 10000), false);
    assert.equal(shouldMarkToolStale('worker.delegate', 10000), false);
  });

  it('respects custom threshold', () => {
    assert.equal(shouldMarkToolStale('workspace.list', 10000, 15000), false);
    assert.equal(shouldMarkToolStale('workspace.list', 16000, 15000), true);
    assert.equal(shouldMarkToolStale('workspace.list', 5000, 15000), false);
  });
});
