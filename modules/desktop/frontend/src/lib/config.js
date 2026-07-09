import { normalizeGatewayBase } from './format.js';

export function gatewayBaseURL() {
  return normalizeGatewayBase(import.meta.env.VITE_RED_PANDA_GATEWAY_BASE_URL);
}
