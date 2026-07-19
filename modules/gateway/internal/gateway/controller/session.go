package controller

import (
	"net/http"
	"strconv"

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
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_session_offset", "message": "offset must be a non-negative integer"}})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_session_limit", "message": "limit must be an integer"}})
		return
	}
	items, err := s.Services.Session.ListPage(offset, limit)
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
	afterSeq, err := strconv.ParseUint(c.DefaultQuery("after_seq", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_history_cursor", "message": "after_seq must be an unsigned integer"}})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "200"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_history_limit", "message": "limit must be an integer"}})
		return
	}
	items, err := s.Services.Session.History(c.Param("id"), afterSeq, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_history_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (s SessionController) Delete(c *gin.Context) {
	if err := s.Services.Session.Delete(c.Param("id")); err != nil {
		status := http.StatusBadRequest
		if err.Error() == "session not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "session_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"deleted": true, "id": c.Param("id")}))
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

func (s SessionController) CompactionState(c *gin.Context) {
	result, err := s.Services.Session.CompactionState(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_compaction_state_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SessionController) Summaries(c *gin.Context) {
	result, err := s.Services.Session.Summaries(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_summaries_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SessionController) ContextState(c *gin.Context) {
	result, err := s.Services.Session.ContextState(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "session_context_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

// Bootstrap returns history + satellites in one response (docs/48 Wave D).
func (s SessionController) Bootstrap(c *gin.Context) {
	id := c.Param("id")
	history, err := s.Services.Session.HistoryAll(id)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "session not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	runs, err := s.Services.Run.ListBySession(id, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	tools, err := s.Services.Tool.ListBySession(id, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	permissions, err := s.Services.Permission.ListBySession(id, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	todos, err := s.Services.Todo.ListBySession(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	contextState, err := s.Services.Session.ContextState(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	goals, err := s.Services.Goal.ListBySession(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "session_bootstrap_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, service.SessionBootstrapResult{
		History:     history,
		Runs:        runs,
		Tools:       tools,
		Permissions: permissions,
		Todos:       todos,
		Context:     contextState,
		Goals:       goals,
	}))
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
