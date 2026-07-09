package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type PermissionController struct {
	Services service.Set
}

func (p PermissionController) Get(c *gin.Context) {
	item, err := p.Services.Permission.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "permission_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (p PermissionController) Pending(c *gin.Context) {
	items, err := p.Services.Permission.Pending(limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "permission_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (p PermissionController) ListByRun(c *gin.Context) {
	items, err := p.Services.Permission.ListByRun(c.Param("id"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "permission_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (p PermissionController) ListBySession(c *gin.Context) {
	items, err := p.Services.Permission.ListBySession(c.Param("id"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "permission_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}
