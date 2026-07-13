import { normalizeGatewayBase } from './format.js';

export function gatewayBaseURL() {
  const env = typeof import.meta !== 'undefined' && import.meta.env
    ? import.meta.env
    : {};
  return normalizeGatewayBase(env.VITE_RED_PANDA_GATEWAY_BASE_URL);
}
