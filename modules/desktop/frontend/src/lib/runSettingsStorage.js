import { defaultRunSettings, normalizeStoredRunSettings } from './runOptions.js';

const RUN_SETTINGS_KEY = 'red_panda_run_settings';

export function loadRunSettings() {
  if (typeof window === 'undefined') {
    return defaultRunSettings;
  }
  try {
    const saved = window.localStorage.getItem(RUN_SETTINGS_KEY);
    if (!saved) {
      return defaultRunSettings;
    }
    return normalizeStoredRunSettings(JSON.parse(saved));
  } catch {
    return defaultRunSettings;
  }
}

export function persistRunSettings(runSettings) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(RUN_SETTINGS_KEY, JSON.stringify(runSettings));
  } catch {
    // Local storage is optional in embedded desktop previews.
  }
}
