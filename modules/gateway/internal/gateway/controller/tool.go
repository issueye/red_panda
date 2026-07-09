package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type ToolController struct {
	Services service.Set
}

func (t ToolController) ListByRun(c *gin.Context) {
	items, err := t.Services.Tool.ListByRun(c.Param("id"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "tool_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (t ToolController) ListBySession(c *gin.Context) {
	items, err := t.Services.Tool.ListBySession(c.Param("id"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "tool_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func limitQuery(c *gin.Context, fallback int) int {
	value := c.Query("limit")
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func uint64Query(c *gin.Context, key string) uint64 {
	value := c.Query(key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
