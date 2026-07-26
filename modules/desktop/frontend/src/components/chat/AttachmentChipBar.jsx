import { ImagePlus, Loader2, X } from 'lucide-react';
import { useRef } from 'react';
import {
  ATTACHMENT_MAX_PER_RUN,
  attachmentImageUrl,
  formatBytes,
} from '../../lib/attachments.js';
import { classNames } from '../../lib/format.js';

/**
 * Composer attachment chip strip (docs/51 §8.1). Lives inside .composer-shell,
 * above the textarea — NOT in .composer-strips (which floats over the timeline
 * and collides with TodoComposerStrip).
 *
 * Each chip is one attachment reference. The strip also owns the "add image"
 * affordance (file picker) since the chips and the trigger are co-located.
 */
export function AttachmentChipBar({
  attachments = [],
  onChange,
  onAddFiles,
  uploading = false,
  disabled = false,
}) {
  const inputRef = useRef(null);

  const atLimit = attachments.length >= ATTACHMENT_MAX_PER_RUN;

  function openPicker() {
    if (disabled || uploading || atLimit) return;
    inputRef.current?.click();
  }

  function handleInputChange(event) {
    const files = Array.from(event.target.files || []);
    event.target.value = '';
    if (files.length) onAddFiles?.(files);
  }

  function removeAt(index) {
    if (disabled) return;
    const next = attachments.slice();
    next.splice(index, 1);
    onChange?.(next);
  }

  if (attachments.length === 0 && disabled && !uploading) {
    // Keep the strip mounted only when it can act: hidden otherwise to keep the
    // pure-text composer visually identical (docs/51 §2.1 goal 7).
    return null;
  }

  return (
    <div className="attachment-chip-bar" data-testid="attachment-chip-bar">
      <input
        accept="image/png,image/jpeg,image/webp,image/gif"
        aria-label="选择图片"
        data-testid="attachment-file-input"
        multiple
        onChange={handleInputChange}
        ref={inputRef}
        style={{ display: 'none' }}
        type="file"
      />
      {attachments.map((attachment, index) => (
        <AttachmentChip
          attachment={attachment}
          disabled={disabled}
          key={attachment.id || attachment.path || attachment.clientKey || index}
          onRemove={() => removeAt(index)}
        />
      ))}
      {uploading ? (
        <span className="attachment-chip attachment-chip-loading" title="上传中…">
          <Loader2 className="spin" size={14} />
          <span>上传中…</span>
        </span>
      ) : null}
      <button
        className={classNames('attachment-add-btn', (disabled || uploading || atLimit) && 'is-disabled')}
        data-testid="attachment-add-btn"
        disabled={disabled || uploading || atLimit}
        onClick={openPicker}
        title={atLimit ? `单次最多 ${ATTACHMENT_MAX_PER_RUN} 张` : '添加图片'}
        type="button"
      >
        <ImagePlus size={14} />
        <span>图片</span>
      </button>
    </div>
  );
}

function AttachmentChip({ attachment, onRemove, disabled }) {
  const url = attachmentImageUrl(attachment);
  const label = attachment.alt || attachment.original_name || attachment.path || '图片';
  const meta = attachment.byte_size ? formatBytes(attachment.byte_size) : '';
  const hasError = Boolean(attachment.error);
  return (
    <div
      className={classNames('attachment-chip', hasError && 'is-error')}
      data-testid="attachment-chip"
      title={hasError ? attachment.error : `${label}${meta ? ` · ${meta}` : ''}`}
    >
      <div className="attachment-chip-thumb">
        {url ? (
          <img alt={label} src={url} />
        ) : hasError ? (
          <span className="attachment-chip-icon">!</span>
        ) : (
          <ImagePlus size={14} />
        )}
      </div>
      <div className="attachment-chip-meta">
        <span className="attachment-chip-name">{label}</span>
        {meta ? <span className="attachment-chip-size">{meta}</span> : null}
        {hasError ? <span className="attachment-chip-error">{attachment.error}</span> : null}
      </div>
      <button
        aria-label={`移除 ${label}`}
        className="attachment-chip-remove"
        disabled={disabled}
        onClick={onRemove}
        type="button"
      >
        <X size={12} />
      </button>
    </div>
  );
}
