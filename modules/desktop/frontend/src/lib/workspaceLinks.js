const URI_SCHEME = /^[a-z][a-z0-9+.-]*:/i;
const WINDOWS_ABSOLUTE = /^[a-z]:\//i;

/** Resolve a Markdown href to a file inside the active workspace. */
export function resolveWorkspaceFileHref(href, workspaceRoot) {
  const root = String(workspaceRoot || '').trim();
  let value = String(href || '').trim();
  if (!root || !value || value.startsWith('#')) return '';

  const hashIndex = value.indexOf('#');
  if (hashIndex >= 0) value = value.slice(0, hashIndex);
  const queryIndex = value.indexOf('?');
  if (queryIndex >= 0) value = value.slice(0, queryIndex);
  try {
    value = decodeURIComponent(value);
  } catch {
    return '';
  }
  value = value.replace(/\\/g, '/').trim();
  if (!value) return '';

  if (WINDOWS_ABSOLUTE.test(value)) return value.replace(/\//g, '\\');
  if (URI_SCHEME.test(value) || value.startsWith('//')) return '';

  const segments = [];
  for (const part of value.replace(/^\/+/, '').split('/')) {
    if (!part || part === '.') continue;
    if (part === '..') {
      if (segments.length === 0) return '';
      segments.pop();
      continue;
    }
    segments.push(part);
  }
  if (segments.length === 0) return '';

  const separator = root.includes('\\') ? '\\' : '/';
  const cleanRoot = root.replace(/[\\/]+$/, '');
  return `${cleanRoot}${separator}${segments.join(separator)}`;
}

