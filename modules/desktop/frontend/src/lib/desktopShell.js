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

/**
 * Reveal a file or folder in the system file explorer (Windows Explorer / Finder / xdg-open).
 * @param {string} path absolute or relative filesystem path
 * @returns {Promise<boolean>} true when the shell accepted the request
 */
export async function openInExplorer(path) {
  const target = typeof path === 'string' ? path.trim() : '';
  if (!target) {
    return false;
  }
  try {
    const bindings = await import('../../bindings/redpanda/desktop/app/app.js');
    if (typeof bindings.OpenInExplorer === 'function') {
      await bindings.OpenInExplorer(target);
      return true;
    }
  } catch {
    // fall through to ByName for older/partial bindings
  }
  try {
    const { ByName } = await import('@wailsio/runtime');
    if (typeof ByName === 'function') {
      await ByName('app.App.OpenInExplorer', target);
      return true;
    }
  } catch {
    // Browser preview / Playwright fixtures do not expose native shell.
  }
  return false;
}
