package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type AppController struct {
	Services service.Set
}

func (a AppController) Bootstrap(c *gin.Context) {
	sessions, err := a.Services.Session.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "bootstrap_sessions_failed", "message": err.Error()}})
		return
	}
	workspace, err := a.Services.Workspace.Current()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "bootstrap_workspace_failed", "message": err.Error()}})
		return
	}
	recent, err := a.Services.Workspace.Recent()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "bootstrap_recent_workspaces_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{
		"gateway": gin.H{
			"version":     a.Services.App.Version,
			"api_version": "v1",
			"ws_url":      "/api/v1/ws",
		},
		"agent_runtime":     a.Services.Run.RuntimeStatus(),
		"workspace":         workspace,
		"recent_workspaces": recent,
		"sessions":          sessions,
		"active_runs":       []any{},
	}))
}
