import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { resolveSubAgentLifecycleStatus } from './subagentStatus.js';

describe('resolveSubAgentLifecycleStatus', () => {
  it('keeps running across tool finished events', () => {
    let status = resolveSubAgentLifecycleStatus('subagent_update', { status: 'running' }, undefined);
    assert.equal(status, 'running');

    status = resolveSubAgentLifecycleStatus('tool_started', { status: 'running' }, status);
    assert.equal(status, 'running');

    status = resolveSubAgentLifecycleStatus('tool_finished', { status: 'completed' }, status);
    assert.equal(status, 'running');

    status = resolveSubAgentLifecycleStatus('tool_finished', { status: 'completed' }, status);
    assert.equal(status, 'running');
  });

  it('only completes on subagent_update', () => {
    let status = 'running';
    status = resolveSubAgentLifecycleStatus('tool_finished', { status: 'completed' }, status);
    assert.equal(status, 'running');

    status = resolveSubAgentLifecycleStatus('subagent_update', { status: 'completed', summary: 'done' }, status);
    assert.equal(status, 'completed');
  });

  it('marks new subagents running from first tool event', () => {
    assert.equal(
      resolveSubAgentLifecycleStatus('tool_started', { status: 'running' }, undefined),
      'running',
    );
    assert.equal(
      resolveSubAgentLifecycleStatus('tool_finished', { status: 'completed' }, undefined),
      'running',
    );
  });

  it('accepts failed/cancelled lifecycle updates', () => {
    assert.equal(
      resolveSubAgentLifecycleStatus('subagent_update', { status: 'failed' }, 'running'),
      'failed',
    );
    assert.equal(
      resolveSubAgentLifecycleStatus('subagent_update', { status: 'cancelled' }, 'cancelling'),
      'cancelled',
    );
  });

  it('accepts queued and starting lifecycle updates', () => {
    assert.equal(
      resolveSubAgentLifecycleStatus('subagent_update', { status: 'queued' }, undefined),
      'queued',
    );
    assert.equal(
      resolveSubAgentLifecycleStatus('subagent_update', { status: 'starting' }, 'queued'),
      'starting',
    );
  });

  it('does not revive completed agents from late tool events', () => {
    assert.equal(
      resolveSubAgentLifecycleStatus('tool_finished', { status: 'completed' }, 'completed'),
      'completed',
    );
  });
});
