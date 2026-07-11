/**
 * Desktop shell helpers that gracefully degrade outside the Wails runtime.
 */

/**
 * Open a native directory picker when running inside Wails.
 * @returns {Promise<string>} selected absolute path, or empty string when cancelled/unavailable
 */
export async function selectDirectory() {
  try {
    const { SelectDirectory } = await import('../../bindings/redpanda/desktop/app/app.js');
    if (typeof SelectDirectory !== 'function') {
      return '';
    }
    const path = await SelectDirectory();
    return typeof path === 'string' ? path : '';
  } catch {
    // Browser preview / Playwright fixtures do not expose native dialogs.
    return '';
  }
}
