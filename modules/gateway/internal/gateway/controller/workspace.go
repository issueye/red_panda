package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type WorkspaceController struct {
	Services service.Set
}

type openWorkspaceRequest struct {
	Root string `json:"root"`
}

func (w WorkspaceController) Open(c *gin.Context) {
	var req openWorkspaceRequest
	if err := c.BindJSON(&req); err != nil || req.Root == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "root is required"}})
		return
	}
	workspace, err := w.Services.Workspace.Open(req.Root)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "workspace_open_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, workspace))
}

func (w WorkspaceController) Current(c *gin.Context) {
	workspace, err := w.Services.Workspace.Current()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "workspace_current_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, workspace))
}

func (w WorkspaceController) Recent(c *gin.Context) {
	items, err := w.Services.Workspace.Recent()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "workspace_recent_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (w WorkspaceController) Delete(c *gin.Context) {
	deleteSessions := c.Query("delete_sessions") != "0" && c.Query("delete_sessions") != "false"
	workspace, deletedSessions, err := w.Services.Workspace.Remove(c.Param("id"), deleteSessions)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "workspace not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "workspace_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{
		"deleted":          true,
		"id":               workspace.ID,
		"root":             workspace.Root,
		"deleted_sessions": deletedSessions,
	}))
}

func (w WorkspaceController) Tree(c *gin.Context) {
	tree, err := w.Services.Workspace.Tree(service.TreeOptions{
		Root:          c.Query("root"),
		Path:          c.Query("path"),
		MaxDepth:      service.ParseDepth(c.Query("max_depth")),
		IncludeHidden: c.Query("include_hidden") == "true",
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "workspace_tree_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, tree))
}

func (w WorkspaceController) File(c *gin.Context) {
	file, err := w.Services.Workspace.File(c.Query("root"), c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "workspace_file_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, file))
}

func (w WorkspaceController) Diff(c *gin.Context) {
	diff, err := w.Services.Workspace.Diff(c.Query("root"), c.Query("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "workspace_diff_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, diff))
}
