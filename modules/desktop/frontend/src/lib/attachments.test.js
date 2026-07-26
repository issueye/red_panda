import assert from 'node:assert/strict';
import test from 'node:test';

import {
  ATTACHMENT_ALLOWED_MIME,
  ATTACHMENT_MAX_BYTES,
  ATTACHMENT_MAX_PER_RUN,
  attachmentImageUrl,
  formatAttachmentRunError,
  formatBytes,
  toRunStartAttachment,
  validateImageFile,
} from './attachments.js';
import { gatewayBase } from './api.js';

test('validateImageFile accepts common image types within size limit', () => {
  const png = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'a.png', { type: 'image/png' });
  assert.equal(validateImageFile(png), null);

  const jpeg = new File([new Uint8Array(10)], 'b.JPG', { type: 'image/jpeg' });
  assert.equal(validateImageFile(jpeg), null);
});

test('validateImageFile rejects non-image and oversized files', () => {
  const txt = new File([new Uint8Array(4)], 'note.txt', { type: 'text/plain' });
  assert.ok(validateImageFile(txt), 'txt must be rejected');

  // Extension-only fallback: a .png with empty MIME is still acceptable.
  const pngNoMime = new File([new Uint8Array(4)], 'a.png', { type: '' });
  assert.equal(validateImageFile(pngNoMime), null);

  const oversized = new File([new Uint8Array(ATTACHMENT_MAX_BYTES + 1)], 'big.png', { type: 'image/png' });
  assert.ok(validateImageFile(oversized), 'oversized must be rejected');
});

test('toRunStartAttachment prefers attachment_id then path, never base64', () => {
  assert.deepEqual(toRunStartAttachment({ id: 'att_1' }), { attachment_id: 'att_1' });
  assert.deepEqual(toRunStartAttachment({ path: 'docs/x.png' }), { path: 'docs/x.png' });
  assert.equal(toRunStartAttachment({}), null);
  // A DTO carrying both must still only emit attachment_id (path is a fallback).
  assert.deepEqual(toRunStartAttachment({ id: 'att_1', path: 'docs/x.png' }), { attachment_id: 'att_1' });
});

test('attachmentImageUrl prefixes the gateway base for relative urls', () => {
  assert.equal(attachmentImageUrl({ url: '/api/v1/attachments/att_1' }), `${gatewayBase}/api/v1/attachments/att_1`);
  assert.equal(attachmentImageUrl({ url: 'https://cdn.test/x.png' }), 'https://cdn.test/x.png');
  assert.equal(attachmentImageUrl({}), '');
  // Optimistic local bubbles may only carry id / attachment_id.
  assert.equal(attachmentImageUrl({ id: 'att_2' }), `${gatewayBase}/api/v1/attachments/att_2`);
  assert.equal(attachmentImageUrl({ attachment_id: 'att_3' }), `${gatewayBase}/api/v1/attachments/att_3`);
});

test('formatAttachmentRunError localizes vision gate failures', () => {
  const text = formatAttachmentRunError('provider profile does not support image input (vision_not_supported)');
  assert.match(text, /支持视觉/);
  assert.equal(formatAttachmentRunError('other error'), 'other error');
});

test('formatBytes renders human-readable sizes', () => {
  assert.equal(formatBytes(0), '0 B');
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 KB');
  assert.equal(formatBytes(5 * 1024 * 1024), '5.0 MB');
});

test('constants mirror Gateway limits (docs/51 §9)', () => {
  assert.equal(ATTACHMENT_MAX_BYTES, 8 * 1024 * 1024);
  assert.equal(ATTACHMENT_MAX_PER_RUN, 6);
  assert.deepEqual(ATTACHMENT_ALLOWED_MIME, ['image/png', 'image/jpeg', 'image/webp', 'image/gif']);
});
