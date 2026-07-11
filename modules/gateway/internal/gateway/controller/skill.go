package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
	"redpanda/protocol/methods"
)

type SkillController struct {
	Services service.Set
}

type skillMutateRequest struct {
	WorkspaceRoot string `json:"workspace_root"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Instructions  string `json:"instructions"`
}

func (s SkillController) List(c *gin.Context) {
	result, err := s.Services.Skills.List(c.Request.Context(), c.Query("workspace_root"))
	if err != nil {
		writeSkillError(c, "list", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SkillController) Get(c *gin.Context) {
	include := c.Query("include_instructions") == "1" || strings.EqualFold(c.Query("include_instructions"), "true")
	result, err := s.Services.Skills.Get(c.Request.Context(), c.Query("workspace_root"), c.Param("name"), include)
	if err != nil {
		writeSkillError(c, "get", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SkillController) Create(c *gin.Context) {
	var req skillMutateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid skill payload"}})
		return
	}
	result, err := s.Services.Skills.Create(c.Request.Context(), methods.SkillMutateParams{
		WorkspaceRoot: req.WorkspaceRoot,
		Name:          req.Name,
		Description:   req.Description,
		Instructions:  req.Instructions,
	})
	if err != nil {
		writeSkillError(c, "create", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SkillController) Update(c *gin.Context) {
	var req skillMutateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid skill payload"}})
		return
	}
	result, err := s.Services.Skills.Update(c.Request.Context(), c.Param("name"), methods.SkillMutateParams{
		WorkspaceRoot: req.WorkspaceRoot,
		Name:          c.Param("name"),
		Description:   req.Description,
		Instructions:  req.Instructions,
	})
	if err != nil {
		writeSkillError(c, "update", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (s SkillController) Delete(c *gin.Context) {
	result, err := s.Services.Skills.Delete(c.Request.Context(), c.Query("workspace_root"), c.Param("name"))
	if err != nil {
		writeSkillError(c, "delete", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func writeSkillError(c *gin.Context, action string, err error) {
	message := err.Error()
	code := "skill_" + action + "_failed"
	status := http.StatusBadRequest
	if strings.Contains(message, "not found") {
		code = "skill_not_found"
		status = http.StatusNotFound
	}
	if strings.Contains(message, "runtime client not configured") {
		status = http.StatusServiceUnavailable
		code = "runtime_unavailable"
	}
	c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": code, "message": message}})
}
