package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type GoalController struct {
	Services service.Set
}

type goalContinueRequest struct {
	Input   string         `json:"input"`
	Options map[string]any `json:"options"`
}

type goalStartRequest struct {
	Objective       string         `json:"objective"`
	Title           string         `json:"title"`
	SuccessCriteria string         `json:"success_criteria"`
	Options         map[string]any `json:"options"`
}

func (g GoalController) ListBySession(c *gin.Context) {
	items, err := g.Services.Goal.ListBySession(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "goal_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"items": items}))
}

func (g GoalController) Get(c *gin.Context) {
	item, err := g.Services.Goal.Get(c.Param("id"), c.Param("goalId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "goal_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (g GoalController) Cancel(c *gin.Context) {
	current, err := g.Services.Goal.Get(c.Param("id"), c.Param("goalId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "goal_not_found", "message": err.Error()}})
		return
	}
	if current.Status == "active" && current.ActiveRunID != "" {
		if err := g.Services.Run.Cancel(c.Request.Context(), current.ActiveRunID, "goal cancelled"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "goal_run_cancel_failed", "message": err.Error()}})
			return
		}
	}
	item, err := g.Services.Goal.CancelGoal(c.Param("id"), c.Param("goalId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "goal_cancel_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (g GoalController) Continue(c *gin.Context) {
	var req goalContinueRequest
	_ = c.ShouldBindJSON(&req)
	result, err := g.Services.Run.ContinueGoal(
		c.Request.Context(),
		c.Param("id"),
		c.Param("goalId"),
		req.Input,
		req.Options,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "goal_continue_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

// Start creates a user-initiated Goal and starts a bound run (Desktop /goal command).
func (g GoalController) Start(c *gin.Context) {
	var req goalStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid goal start payload"}})
		return
	}
	result, err := g.Services.Run.StartGoal(
		c.Request.Context(),
		c.Param("id"),
		req.Objective,
		req.Title,
		req.SuccessCriteria,
		req.Options,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "goal_start_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}
