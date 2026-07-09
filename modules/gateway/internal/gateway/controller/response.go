package controller

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	protows "redpanda/protocol/ws"
)

func envelope(c *gin.Context, data any) gin.H {
	return gin.H{
		"ok":         true,
		"data":       data,
		"error":      nil,
		"request_id": c.GetString("request_id"),
	}
}

func response(id string, payload any) protows.Envelope {
	ok := true
	raw, _ := json.Marshal(payload)
	return protows.Envelope{ID: id, Type: protows.TypeResponse, OK: &ok, Payload: raw}
}

func errorMessage(id, code, message string) protows.Envelope {
	ok := false
	return protows.Envelope{
		ID:   id,
		Type: protows.TypeError,
		OK:   &ok,
		Error: &protows.Error{
			Code:        code,
			Message:     message,
			Recoverable: true,
		},
	}
}
