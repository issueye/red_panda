import { X } from 'lucide-react';
import { useEffect, useState } from 'react';
import { attachmentImageUrl, formatBytes } from '../../lib/attachments.js';
import { classNames } from '../../lib/format.js';

/**
 * Renders image thumbnails for a message's attachments, with a click-to-zoom
 * lightbox (docs/51 §8.2). The Desktop never receives base64 in hydrate, so
 * each thumb loads via the authenticated attachment URL.
 *
 * @param {{ attachments?: any[], workspaceRoot?: string }} props
 */
export function AttachmentThumbs({ attachments = [] }) {
  const [lightbox, setLightbox] = useState(null);
  if (!attachments.length) return null;

  return (
    <>
      <div className="message-attachments" data-testid="message-attachments">
        {attachments.map((attachment, index) => (
          <AttachmentThumb
            attachment={attachment}
            key={attachment.id || attachment.path || index}
            onOpen={() => setLightbox(attachment)}
          />
        ))}
      </div>
      {lightbox ? <AttachmentLightbox attachment={lightbox} onClose={() => setLightbox(null)} /> : null}
    </>
  );
}

function AttachmentThumb({ attachment, onOpen }) {
  const url = attachmentImageUrl(attachment);
  const label = attachment.alt || attachment.original_name || attachment.path || '图片';
  const meta = attachment.byte_size ? formatBytes(attachment.byte_size) : '';
  const title = `${label}${meta ? ` · ${meta}` : ''}`;
  if (!url) {
    // Workspace path reference (no upload id): show a labeled placeholder.
    return (
      <div className="message-attachment-thumb is-placeholder" title={title}>
        <span>{label}</span>
      </div>
    );
  }
  return (
    <button
      className="message-attachment-thumb"
      data-testid="message-attachment-thumb"
      onClick={onOpen}
      title={title}
      type="button"
    >
      <img alt={label} loading="lazy" src={url} />
    </button>
  );
}

function AttachmentLightbox({ attachment, onClose }) {
  const url = attachmentImageUrl(attachment);
  const label = attachment.alt || attachment.original_name || attachment.path || '图片';
  const meta = attachment.byte_size ? ` · ${formatBytes(attachment.byte_size)}` : '';

  useEffect(() => {
    function onKey(event) {
      if (event.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  if (!url) return null;

  return (
    <div
      className={classNames('attachment-lightbox')}
      data-testid="attachment-lightbox"
      onClick={onClose}
      role="dialog"
      aria-label={`${label}${meta}`}
    >
      <button
        aria-label="关闭"
        className="attachment-lightbox-close"
        onClick={onClose}
        type="button"
      >
        <X size={18} />
      </button>
      <img
        alt={label}
        onClick={(event) => event.stopPropagation()}
        src={url}
      />
      <div className="attachment-lightbox-caption">
        {label}
        {meta}
      </div>
    </div>
  );
}
