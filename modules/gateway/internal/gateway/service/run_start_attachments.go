// Attachment validation, resolution, and limits for run.start (docs/51 §6.2, §9).
// This file wires image attachments into the existing admit → prepare → dispatch
// pipeline in run_start.go without changing the pure-text path.

package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"redpanda/protocol/methods"
)

const (
	// maxAttachmentsPerRun is the per-run.start image cap (docs/51 §9).
	maxAttachmentsPerRun = 6
	// maxRunInlineBytes caps the total inline bytes carried to the Runtime in a
	// single run (docs/51 §4 Strategy A). Enforced at admission before inlining.
	maxRunInlineBytes = 24 * 1024 * 1024
)

// resolvedAttachment is a validated attachment reference ready to persist in a
// user message and to surface to the Runtime.
type resolvedAttachment struct {
	attachmentID string
	path         string // workspace-relative
	mime         string
	width        int
	height       int
	byteSize     int64
	alt          string
	// absPath is the absolute filesystem path used for Strategy A inlining
	// (docs/51 §4). Empty when the attachment cannot be read (should not
	// happen after successful resolve).
	absPath string
}

// parseInputAttachments extracts attachment references from the run.start input
// map. Only attachment_id / path are honored from the wire; data_b64 is ignored
// (Desktop must never send base64, docs/51 §5.3).
func parseInputAttachments(input map[string]any) []map[string]any {
	if input == nil {
		return nil
	}
	raw, ok := input["attachments"]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []map[string]any:
		return value
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items
	default:
		return nil
	}
}

// validateAndResolveAttachments turns raw wire refs into resolved references:
// attachment_id refs are ownership-checked against the session; path refs are
// resolved through the workspace (symlink-safe) and sniffed for an allowed MIME.
// Schedule-triggered runs are refused attachments (docs/51 §9 permission gate).
func (r RunService) validateAndResolveAttachments(admission *runAdmission, raw []map[string]any, options map[string]any) ([]resolvedAttachment, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	triggerSource := strings.TrimSpace(stringOption(options, "trigger_source"))
	if triggerSource == "schedule" {
		return nil, errors.New("scheduled runs cannot carry image attachments")
	}
	if len(raw) > maxAttachmentsPerRun {
		return nil, fmt.Errorf("too many attachments: %d (max %d)", len(raw), maxAttachmentsPerRun)
	}

	resolved := make([]resolvedAttachment, 0, len(raw))
	var totalBytes int64
	for _, ref := range raw {
		attachmentID := strings.TrimSpace(strVal(ref, "attachment_id"))
		path := strings.TrimSpace(strVal(ref, "path"))
		if attachmentID == "" && path == "" {
			return nil, errors.New("attachment requires attachment_id or path")
		}
		if attachmentID != "" && path != "" {
			return nil, errors.New("attachment must specify attachment_id or path, not both")
		}
		var ra resolvedAttachment
		var err error
		if attachmentID != "" {
			ra, err = r.resolveAttachmentByID(admission.session.ID, attachmentID)
		} else {
			ra, err = r.resolveAttachmentByPath(admission.session.WorkspaceRoot, path)
		}
		if err != nil {
			return nil, err
		}
		totalBytes += ra.byteSize
		if totalBytes > maxRunInlineBytes {
			return nil, fmt.Errorf("attachments exceed %d bytes in a single run", maxRunInlineBytes)
		}
		resolved = append(resolved, ra)
	}
	return resolved, nil
}

func (r RunService) resolveAttachmentByID(sessionID, attachmentID string) (resolvedAttachment, error) {
	row, err := r.repos.Attachments.Get(attachmentID, sessionID)
	if err != nil {
		return resolvedAttachment{}, fmt.Errorf("attachment %s not found in session: %w", attachmentID, err)
	}
	// Touch recency for GC heuristics.
	_ = r.repos.Attachments.TouchLastRef(attachmentID, nowUTC())
	absPath := ""
	if r.attachments != nil {
		absPath = r.attachments.AbsoluteStoragePath(row.StoragePath)
	}
	return resolvedAttachment{
		attachmentID: row.ID,
		mime:         row.MIME,
		width:        row.Width,
		height:       row.Height,
		byteSize:     row.ByteSize,
		alt:          row.OriginalName,
		absPath:      absPath,
	}, nil
}

func (r RunService) resolveAttachmentByPath(workspaceRoot, relPath string) (resolvedAttachment, error) {
	if r.attachments == nil {
		return resolvedAttachment{}, errors.New("attachment service is not configured")
	}
	// Resolve via AttachmentService (lexical + symlink eval, docs/51 §4).
	abs, err := r.attachments.ResolveWorkspacePathAbs(workspaceRoot, relPath)
	if err != nil {
		return resolvedAttachment{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return resolvedAttachment{}, fmt.Errorf("workspace image %s: %w", relPath, err)
	}
	if info.IsDir() {
		return resolvedAttachment{}, fmt.Errorf("workspace path %s is a directory", relPath)
	}
	mimeStr, _, width, height, err := sniffImageFile(abs)
	if err != nil {
		return resolvedAttachment{}, err
	}
	return resolvedAttachment{
		path:     relPath,
		mime:     mimeStr,
		width:    width,
		height:   height,
		byteSize: info.Size(),
		alt:      relPath,
		absPath:  abs,
	}, nil
}

// nowUTC is a small shared helper to avoid importing time at multiple sites.
func nowUTC() time.Time { return time.Now().UTC() }

// visionGate rejects a run that carries attachments when the resolved provider
// profile does not support vision (docs/51 §6.5). Called after profile apply.
func visionGate(params methods.RunExecuteParams, resolved []resolvedAttachment) error {
	if len(resolved) == 0 {
		return nil
	}
	if params.Options.SupportsVision {
		return nil
	}
	return errors.New("provider profile does not support image input (vision_not_supported)")
}

// userMessageContent builds the persisted user message blocks: a single text
// block plus one image_ref per resolved attachment (docs/51 §5.2).
func userMessageContent(text string, attachments []resolvedAttachment) []methods.ContentBlock {
	blocks := make([]methods.ContentBlock, 0, 1+len(attachments))
	blocks = append(blocks, methods.ContentBlock{Type: "text", Text: text})
	for _, att := range attachments {
		blocks = append(blocks, methods.ContentBlock{
			Type:         "image_ref",
			AttachmentID: att.attachmentID,
			Path:         att.path,
			MIME:         att.mime,
			Alt:          att.alt,
			Width:        att.width,
			Height:       att.height,
			ByteSize:     att.byteSize,
		})
	}
	return blocks
}

// toInputAttachments maps resolved attachments to the ReplyInput wire form.
// Strategy A (docs/51 §4 / docs/52 Slice C): when supportsVision is true, inline
// DataB64 from disk (ephemeral Gateway→Runtime only; never persisted on message
// rows). Total inline budget is enforced earlier by maxRunInlineBytes.
func toInputAttachments(resolved []resolvedAttachment, supportsVision bool) []methods.InputAttachment {
	if len(resolved) == 0 {
		return nil
	}
	out := make([]methods.InputAttachment, 0, len(resolved))
	for _, att := range resolved {
		item := methods.InputAttachment{
			AttachmentID: att.attachmentID,
			Path:         att.path,
			MIME:         att.mime,
			ByteSize:     att.byteSize,
		}
		if supportsVision && att.absPath != "" {
			if raw, err := os.ReadFile(att.absPath); err == nil && len(raw) > 0 {
				item.DataB64 = base64.StdEncoding.EncodeToString(raw)
				if item.ByteSize == 0 {
					item.ByteSize = int64(len(raw))
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func strVal(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
