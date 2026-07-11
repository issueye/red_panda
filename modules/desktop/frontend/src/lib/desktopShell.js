/**
 * Desktop shell helpers that gracefully degrade outside the Wails runtime.
 */

async function getCurrentWindow() {
  try {
    const runtime = await import('@wailsio/runtime');
    return runtime.Window || null;
  } catch {
    return null;
  }
}

/**
 * Minimise the current desktop window.
 * @returns {Promise<boolean>}
 */
export async function windowMinimise() {
  try {
    const win = await getCurrentWindow();
    if (!win?.Minimise) return false;
    await win.Minimise();
    return true;
  } catch {
    return false;
  }
}

/**
 * Toggle maximised / restored state for the current desktop window.
 * @returns {Promise<boolean>}
 */
export async function windowToggleMaximise() {
  try {
    const win = await getCurrentWindow();
    if (!win?.ToggleMaximise) return false;
    await win.ToggleMaximise();
    return true;
  } catch {
    return false;
  }
}

/**
 * Close the current desktop window.
 * @returns {Promise<boolean>}
 */
export async function windowClose() {
  try {
    const win = await getCurrentWindow();
    if (!win?.Close) return false;
    await win.Close();
    return true;
  } catch {
    return false;
  }
}

/**
 * @returns {Promise<boolean>} whether the current window is maximised
 */
export async function windowIsMaximised() {
  try {
    const win = await getCurrentWindow();
    if (!win?.IsMaximised) return false;
    return Boolean(await win.IsMaximised());
  } catch {
    return false;
  }
}

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
