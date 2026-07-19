import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import {
  clampLeftPanelWidth,
  clampRightPanelWidth,
  LEFT_PANEL_WIDTH_DEFAULT,
  LEFT_PANEL_WIDTH_MAX,
  LEFT_PANEL_WIDTH_MIN,
  RIGHT_PANEL_WIDTH_DEFAULT,
  RIGHT_PANEL_WIDTH_MAX,
  RIGHT_PANEL_WIDTH_MIN,
} from './panelLayout.js';

describe('panelLayout clamps', () => {
  it('clamps left panel to bounds', () => {
    assert.equal(clampLeftPanelWidth(Number.NaN), LEFT_PANEL_WIDTH_DEFAULT);
    assert.equal(clampLeftPanelWidth(10), LEFT_PANEL_WIDTH_MIN);
    assert.equal(clampLeftPanelWidth(9999), LEFT_PANEL_WIDTH_MAX);
    assert.equal(clampLeftPanelWidth(300.4), 300);
  });

  it('clamps right panel to bounds', () => {
    assert.equal(clampRightPanelWidth(undefined), RIGHT_PANEL_WIDTH_DEFAULT);
    assert.equal(clampRightPanelWidth(10), RIGHT_PANEL_WIDTH_MIN);
    assert.equal(clampRightPanelWidth(9999), RIGHT_PANEL_WIDTH_MAX);
    assert.equal(clampRightPanelWidth(340.6), 341);
  });
});
