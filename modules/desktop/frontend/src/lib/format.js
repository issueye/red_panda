export function classNames(...items) {
  return items.filter(Boolean).join(' ');
}

export function formatSeq(value) {
  if (!Number.isFinite(Number(value))) return '0';
  return String(value).padStart(3, '0');
}

export function normalizeGatewayBase(value) {
  const base = String(value || 'http://127.0.0.1:17888').replace(/\/+$/, '');
  return base;
}
