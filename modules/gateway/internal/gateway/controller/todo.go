package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type TodoController struct {
	Services service.Set
}

func (t TodoController) ListBySession(c *gin.Context) {
	result, err := t.Services.Todo.ListBySession(c.Param("id"))
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "session not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": "todo_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}
