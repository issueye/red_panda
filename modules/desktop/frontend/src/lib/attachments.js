import { gatewayBase } from './api.js';

// Client-side guards that mirror the Gateway limits (docs/51 §9). The server
// remains the source of truth; these avoid wasted round-trips and give an
// early, localized error message.
export const ATTACHMENT_MAX_BYTES = 8 * 1024 * 1024; // 8 MiB per file
export const ATTACHMENT_MAX_PER_RUN = 6;
export const ATTACHMENT_ALLOWED_MIME = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
const ATTACHMENT_ALLOWED_EXTENSIONS = ['.png', '.jpg', '.jpeg', '.webp', '.gif'];

/**
 * Validate an image file before upload. Returns null when acceptable, or a
 * localized error string describing the first problem.
 * @param {File} file
 * @returns {string | null}
 */
export function validateImageFile(file) {
  if (!file) return '请选择图片文件';
  const name = (file.name || '').toLowerCase();
  const hasExt = ATTACHMENT_ALLOWED_EXTENSIONS.some((ext) => name.endsWith(ext));
  // Trust MIME first, fall back to extension (some paste paths lack MIME).
  const mimeOk = ATTACHMENT_ALLOWED_MIME.includes(file.type);
  if (!mimeOk && !hasExt) {
    return '仅支持 PNG / JPEG / WebP / GIF 图片';
  }
  if (file.size <= 0) return '图片为空';
  if (file.size > ATTACHMENT_MAX_BYTES) {
    return `图片过大（${formatBytes(file.size)}），单张上限 8 MiB`;
  }
  return null;
}

/**
 * Upload an image to a session via multipart/form-data (docs/51 §6.1).
 * Returns the AttachmentDTO (id, mime, byte_size, url, …).
 * @param {string} sessionId
 * @param {File | Blob} file
 * @param {{ alt?: string, signal?: AbortSignal }} [options]
 * @returns {Promise<object>}
 */
export async function uploadAttachment(sessionId, file, options = {}) {
  if (!sessionId) throw new Error('缺少会话，无法上传图片');
  const alt = options.alt || file?.name || 'image';
  const form = new FormData();
  form.append('alt', alt);
  form.append('file', file, file?.name || alt);

  const response = await fetch(`${gatewayBase}/api/v1/sessions/${sessionId}/attachments`, {
    method: 'POST',
    body: form,
    signal: options.signal,
  });
  const body = await response.json().catch(() => null);
  if (!response.ok || body?.ok === false) {
    const message = body?.error?.message || `上传失败 (HTTP ${response.status})`;
    throw new Error(message);
  }
  return body.data;
}

/**
 * Build a run.start-ready attachment reference. The Desktop→Gateway wire only
 * ever carries the reference, never base64 (docs/51 §5.3).
 * @param {object} attachment AttachmentDTO from uploadAttachment / history
 */
export function toRunStartAttachment(attachment) {
  if (!attachment) return null;
  if (attachment.id) return { attachment_id: attachment.id };
  if (attachment.path) return { path: attachment.path };
  return null;
}

/**
 * Resolve an attachment into an authenticated-ish URL for <img src>. Desktop
 * runs the gateway without a token locally, so the bare URL works. Absolute
 * attachment.url values from the gateway are relative paths; prefix the base.
 * @param {object} attachment
 */
export function attachmentImageUrl(attachment) {
  const url = attachment?.url;
  if (!url) return '';
  if (/^https?:/i.test(url)) return url;
  return `${gatewayBase}${url}`;
}

/**
 * Human-readable byte size.
 * @param {number} bytes
 */
export function formatBytes(bytes) {
  const n = Number(bytes) || 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
