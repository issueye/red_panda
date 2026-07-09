package app

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = "req_" + strings.ReplaceAll(c.ClientIP(), ":", "_")
		}
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func auth(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		header := c.GetHeader("Authorization")
		if header != "Bearer "+token {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"ok": false,
				"error": gin.H{
					"code":    "unauthorized",
					"message": "unauthorized",
				},
				"request_id": c.GetString("request_id"),
			})
			return
		}
		c.Next()
	}
}
