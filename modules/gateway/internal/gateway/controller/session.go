package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type SessionController struct {
	Services service.Set
}

type createSessionRequest struct {
	Name          string `json:"name"`
	WorkspaceRoot string `json:"workspace_root"`
}

func (s SessionController) List(c *gin.Context) {
	items, err := s.Services.Session.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (s SessionController) Create(c *gin.Context) {
	var req createSessionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid session payload"}})
		return
	}
	session, err := s.Services.Session.Create(req.Name, req.WorkspaceRoot)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, session))
}

func (s SessionController) History(c *gin.Context) {
	items, err := s.Services.Session.History(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_history_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (s SessionController) Fork(c *gin.Context) {
	var req service.ForkSessionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid fork payload"}})
		return
	}
	result, err := s.Services.Session.Fork(c.Param("id"), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_fork_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SessionController) CompactPreview(c *gin.Context) {
	var req service.CompactPreviewRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid compact preview payload"}})
		return
	}
	result, err := s.Services.Session.CompactPreview(c.Param("id"), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_compact_preview_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SessionController) Compact(c *gin.Context) {
	var req service.CompactSessionRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid compact payload"}})
		return
	}
	result, err := s.Services.Session.Compact(c.Param("id"), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_compact_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}
