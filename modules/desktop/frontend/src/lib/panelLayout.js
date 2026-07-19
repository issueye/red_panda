/**
 * Side panel width persistence and clamping (docs/47 Wave D).
 * Extracted from App.jsx so layout math is unit-testable without the shell.
 */

export const RIGHT_PANEL_WIDTH_KEY = 'red_panda_right_panel_width';
export const RIGHT_PANEL_WIDTH_DEFAULT = 300;
export const RIGHT_PANEL_WIDTH_MIN = 220;
export const RIGHT_PANEL_WIDTH_MAX = 560;

export const LEFT_PANEL_WIDTH_KEY = 'red_panda_left_panel_width';
export const LEFT_PANEL_WIDTH_DEFAULT = 280;
export const LEFT_PANEL_WIDTH_MIN = 220;
export const LEFT_PANEL_WIDTH_MAX = 480;

export const rightPanelTabs = [
  { id: 'workers', label: 'Worker', testId: 'right-tab-workers' },
  { id: 'activity', label: '活动', testId: 'right-tab-activity' },
  { id: 'memory', label: '记忆', testId: 'right-tab-memory' },
];

export function clampLeftPanelWidth(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return LEFT_PANEL_WIDTH_DEFAULT;
  return Math.min(LEFT_PANEL_WIDTH_MAX, Math.max(LEFT_PANEL_WIDTH_MIN, Math.round(n)));
}

export function clampRightPanelWidth(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return RIGHT_PANEL_WIDTH_DEFAULT;
  return Math.min(RIGHT_PANEL_WIDTH_MAX, Math.max(RIGHT_PANEL_WIDTH_MIN, Math.round(n)));
}

export function loadLeftPanelWidth() {
  if (typeof window === 'undefined') return LEFT_PANEL_WIDTH_DEFAULT;
  try {
    const saved = window.localStorage.getItem(LEFT_PANEL_WIDTH_KEY);
    return saved == null ? LEFT_PANEL_WIDTH_DEFAULT : clampLeftPanelWidth(saved);
  } catch {
    return LEFT_PANEL_WIDTH_DEFAULT;
  }
}

export function loadRightPanelWidth() {
  if (typeof window === 'undefined') {
    return RIGHT_PANEL_WIDTH_DEFAULT;
  }
  try {
    const saved = window.localStorage.getItem(RIGHT_PANEL_WIDTH_KEY);
    return saved == null ? RIGHT_PANEL_WIDTH_DEFAULT : clampRightPanelWidth(saved);
  } catch {
    return RIGHT_PANEL_WIDTH_DEFAULT;
  }
}

export function persistLeftPanelWidth(width) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(LEFT_PANEL_WIDTH_KEY, String(width));
  } catch {
    // Local storage is optional in embedded desktop previews.
  }
}

export function persistRightPanelWidth(width) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(RIGHT_PANEL_WIDTH_KEY, String(width));
  } catch {
    // Local storage is optional in embedded desktop previews.
  }
}
