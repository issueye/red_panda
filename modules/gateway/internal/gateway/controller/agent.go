package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type AgentController struct {
	Services service.Set
}

func (a AgentController) Status(c *gin.Context) {
	c.JSON(http.StatusOK, envelope(c, a.Services.Run.RuntimeStatus()))
}
