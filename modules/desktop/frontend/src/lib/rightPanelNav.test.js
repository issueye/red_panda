import assert from 'node:assert/strict';
import test from 'node:test';
import { nextRightPanelTabIndex } from './rightPanelNav.js';

test('nextRightPanelTabIndex wraps arrows and handles Home/End', () => {
  assert.equal(nextRightPanelTabIndex(0, 'ArrowRight', 3), 1);
  assert.equal(nextRightPanelTabIndex(2, 'ArrowRight', 3), 0);
  assert.equal(nextRightPanelTabIndex(0, 'ArrowLeft', 3), 2);
  assert.equal(nextRightPanelTabIndex(1, 'Home', 3), 0);
  assert.equal(nextRightPanelTabIndex(0, 'End', 3), 2);
  assert.equal(nextRightPanelTabIndex(1, 'Enter', 3), null);
  assert.equal(nextRightPanelTabIndex(-1, 'ArrowRight', 3), null);
});
