package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type ScheduleController struct {
	Services service.Set
}

func (s ScheduleController) List(c *gin.Context) {
	enabledOnly := c.Query("enabled") == "true" || c.Query("enabled") == "1"
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := s.Services.Schedule.List(enabledOnly, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "schedule_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (s ScheduleController) Create(c *gin.Context) {
	var req service.ScheduleCreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid schedule payload"}})
		return
	}
	item, err := s.Services.Schedule.Create(req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "schedule_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Get(c *gin.Context) {
	item, err := s.Services.Schedule.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "schedule_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Update(c *gin.Context) {
	var req service.ScheduleUpdateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid schedule payload"}})
		return
	}
	item, err := s.Services.Schedule.Update(c.Param("id"), req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "schedule_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Delete(c *gin.Context) {
	item, err := s.Services.Schedule.Delete(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "schedule_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Enable(c *gin.Context) {
	item, err := s.Services.Schedule.SetEnabled(c.Param("id"), true)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "schedule_enable_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Disable(c *gin.Context) {
	item, err := s.Services.Schedule.SetEnabled(c.Param("id"), false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "schedule_disable_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (s ScheduleController) Trigger(c *gin.Context) {
	result, err := s.Services.Schedule.Trigger(c.Request.Context(), c.Param("id"))
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "schedule_trigger_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s ScheduleController) ListRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := s.Services.Schedule.ListRuns(c.Param("id"), limit)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "schedule_runs_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}
