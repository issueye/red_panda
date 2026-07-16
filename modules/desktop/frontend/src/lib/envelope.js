/**
 * Shared Gateway list/envelope helpers (docs/41 W2-4).
 * Keeps domain normalize*List functions thin and consistent.
 */

/**
 * Extract a raw array from common Gateway list envelopes.
 * Accepts bare arrays, { items }, { data }, and optional alternate keys
 * (e.g. notes, servers, profiles).
 *
 * @param {unknown} data
 * @param {{ keys?: string[] }} [options]
 * @returns {any[]}
 */
export function listFromEnvelope(data, { keys = ['items'] } = {}) {
  if (Array.isArray(data)) return data;
  if (!data || typeof data !== 'object') return [];
  for (const key of keys) {
    if (Array.isArray(data[key])) return data[key];
  }
  if (Array.isArray(data.data)) return data.data;
  return [];
}

/**
 * Map list envelope entries through a domain normalizer.
 *
 * @template T
 * @param {unknown} data
 * @param {(item: any) => T} mapFn
 * @param {{ keys?: string[] }} [options]
 * @returns {T[]}
 */
export function mapListFromEnvelope(data, mapFn, options) {
  return listFromEnvelope(data, options).map(mapFn);
}
