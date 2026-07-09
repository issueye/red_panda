package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type HealthController struct {
	Services service.Set
}

func (h HealthController) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, envelope(c, gin.H{
		"service": "red-panda-gateway",
		"version": h.Services.App.Version,
	}))
}

func (h HealthController) Readyz(c *gin.Context) {
	c.JSON(http.StatusOK, envelope(c, gin.H{
		"gateway":       "ready",
		"agent_runtime": h.Services.Run.RuntimeStatus(),
	}))
}
