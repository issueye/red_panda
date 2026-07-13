import { gatewayBaseURL } from './config.js';

export const gatewayBase = gatewayBaseURL();

/**
 * JSON API helper for Gateway HTTP endpoints.
 * Expects the standard { ok, data, error } envelope.
 */
export async function apiJson(path, options = {}) {
  const response = await fetch(`${gatewayBase}${path}`, {
    headers: { 'content-type': 'application/json', ...(options.headers || {}) },
    ...options,
  });
  const body = await response.json();
  if (!response.ok || body.ok === false) {
    throw new Error(body.error?.message || `HTTP ${response.status}`);
  }
  return body.data;
}
