package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type RunController struct {
	Services service.Set
}

func (r RunController) Get(c *gin.Context) {
	item, err := r.Services.Run.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "run_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (r RunController) ListBySession(c *gin.Context) {
	items, err := r.Services.Run.ListBySession(c.Param("id"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "run_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (r RunController) Events(c *gin.Context) {
	items, err := r.Services.Run.Events(c.Param("id"), uint64Query(c, "after_seq"), limitQuery(c, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "run_events_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}
