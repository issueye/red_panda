package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type MemoryController struct {
	Services service.Set
}

type memoryCreateRequest struct {
	Scope           string         `json:"scope"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	Title           string         `json:"title"`
	Content         string         `json:"content"`
	Confidence      string         `json:"confidence"`
	WorkspaceRoot   string         `json:"workspace_root"`
	SessionID       string         `json:"session_id"`
	RunID           string         `json:"run_id"`
	SourceEventID   string         `json:"source_event_id"`
	SourceMessageID string         `json:"source_message_id"`
	Source          string         `json:"source"`
	Metadata        map[string]any `json:"metadata"`
}

type memoryUpdateRequest struct {
	Scope      *string        `json:"scope"`
	Kind       *string        `json:"kind"`
	Status     *string        `json:"status"`
	Title      *string        `json:"title"`
	Content    *string        `json:"content"`
	Confidence *string        `json:"confidence"`
	Metadata   map[string]any `json:"metadata"`
}

type memoryPreviewRunRequest struct {
	SessionID     string `json:"session_id"`
	WorkspaceRoot string `json:"workspace_root"`
	Input         string `json:"input"`
}

func (m MemoryController) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := m.Services.Memory.List(service.MemoryListRequest{
		Scope:         c.Query("scope"),
		WorkspaceRoot: c.Query("workspace_root"),
		SessionID:     c.Query("session_id"),
		Status:        c.Query("status"),
		Limit:         limit,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "memory_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (m MemoryController) Create(c *gin.Context) {
	var req memoryCreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid memory payload"}})
		return
	}
	item, err := m.Services.Memory.Create(service.MemoryCreateRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "memory_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MemoryController) Update(c *gin.Context) {
	var req memoryUpdateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid memory payload"}})
		return
	}
	item, err := m.Services.Memory.Update(c.Param("id"), service.MemoryUpdateRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "memory_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MemoryController) Delete(c *gin.Context) {
	item, err := m.Services.Memory.Delete(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "memory_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MemoryController) PreviewRun(c *gin.Context) {
	var req memoryPreviewRunRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid memory preview payload"}})
		return
	}
	result, err := m.Services.Memory.PreviewRun(service.MemoryPreviewRunRequest(req))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "memory_preview_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}
