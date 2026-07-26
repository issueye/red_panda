package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/service"
)

// AttachmentController exposes the upload/meta/bytes/delete HTTP surface for
// session image assets (docs/51 §6.1). Binary bytes are owned by
// AttachmentService; this layer only handles multipart parsing + envelope.
type AttachmentController struct {
	Services service.Set
}

const attachmentMaxMemory = 4 << 20 // 4 MiB in-memory threshold; rest spills to temp file

// Upload handles multipart POST /api/v1/sessions/:id/attachments.
// Form fields: file (required), alt (optional).
func (a AttachmentController) Upload(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "session id is required")
		return
	}
	if err := c.Request.ParseMultipartForm(attachmentMaxMemory); err != nil {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "multipart form parse failed: "+err.Error())
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "file field is required")
		return
	}
	defer file.Close()

	alt := strings.TrimSpace(c.Request.PostFormValue("alt"))
	originalName := ""
	if header != nil {
		originalName = header.Filename
	}
	if alt == "" {
		alt = originalName
	}

	dto, err := a.Services.Attachments.Store(service.AttachmentStoreRequest{
		SessionID:    sessionID,
		Reader:       file,
		OriginalName: originalName,
		Alt:          alt,
		CreatedBy:    "user",
	})
	if err != nil {
		status, code := attachmentErrorClass(err)
		attachmentError(c, status, code, err.Error())
		return
	}
	c.JSON(http.StatusCreated, envelope(c, dto))
}

// List handles GET /api/v1/sessions/:id/attachments (metadata only, no bytes).
func (a AttachmentController) List(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "session id is required")
		return
	}
	items, err := a.Services.Attachments.ListBySession(sessionID)
	if err != nil {
		attachmentError(c, http.StatusInternalServerError, "attachment_list_failed", err.Error())
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"items": items}))
}


// FromWorkspace handles POST /api/v1/sessions/:id/attachments/from-workspace.
// Body: {"path": "relative/to/workspace.png"}. Caches the workspace image as an
// attachment (sha256-deduped) so Desktop can treat path refs and uploads uniformly
// when desired (docs/52 Slice D). Path-only refs without caching remain valid via
// run.start input.attachments[{path}].
func (a AttachmentController) FromWorkspace(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "session id is required")
		return
	}
	var req struct {
		Path          string `json:"path"`
		WorkspaceRoot string `json:"workspace_root"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "invalid from-workspace payload")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		attachmentError(c, http.StatusBadRequest, "invalid_payload", "path is required")
		return
	}
	dto, err := a.Services.Attachments.StoreFromWorkspace(sessionID, req.WorkspaceRoot, req.Path)
	if err != nil {
		status, code := attachmentErrorClass(err)
		attachmentError(c, status, code, err.Error())
		return
	}
	c.JSON(http.StatusCreated, envelope(c, dto))
}

// Meta handles GET /api/v1/attachments/:id/meta.
func (a AttachmentController) Meta(c *gin.Context) {
	dto, err := a.Services.Attachments.Meta(c.Param("id"), sessionScope(c))
	if err != nil {
		status, code := attachmentErrorClass(err)
		attachmentError(c, status, code, err.Error())
		return
	}
	c.JSON(http.StatusOK, envelope(c, dto))
}

// Bytes handles GET /api/v1/attachments/:id — streams the raw image.
func (a AttachmentController) Bytes(c *gin.Context) {
	mimeStr, data, sha, err := a.Services.Attachments.Bytes(c.Param("id"), sessionScope(c))
	if err != nil {
		status, code := attachmentErrorClass(err)
		attachmentError(c, status, code, err.Error())
		return
	}
	c.Header("Content-Type", mimeStr)
	c.Header("Cache-Control", "private, max-age=3600")
	if sha != "" {
		c.Header("ETag", `"`+sha+`"`)
	}
	c.Data(http.StatusOK, mimeStr, data)
}

// Delete handles DELETE /api/v1/attachments/:id.
func (a AttachmentController) Delete(c *gin.Context) {
	if err := a.Services.Attachments.Delete(c.Param("id"), sessionScope(c)); err != nil {
		status, code := attachmentErrorClass(err)
		attachmentError(c, status, code, err.Error())
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"deleted": true}))
}

// sessionScope returns the session id to scope attachment reads by when the
// caller passes ?session_id=. Attachment GET without a session scope is still
// allowed (owner-scoping happens in the service for upload/list).
func sessionScope(c *gin.Context) string {
	return strings.TrimSpace(c.Query("session_id"))
}

func attachmentErrorClass(err error) (int, string) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound, "attachment_not_found"
	case isClientAttachmentError(err):
		return http.StatusBadRequest, "attachment_invalid"
	default:
		return http.StatusInternalServerError, "attachment_failed"
	}
}

func attachmentError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": code, "message": message}})
}

// isClientAttachmentError returns true for validation failures the service
// surfaces as plain errors (string matching keeps the service API simple).
func isClientAttachmentError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, prefix := range []string{
		"attachment exceeds",
		"file is not a supported image",
		"unsupported image type",
		"session attachment quota exceeded",
		"path must be relative",
		"path escapes workspace root",
		"path is required",
		"workspace image",
		"workspace path",
		"file is not a supported image",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}
