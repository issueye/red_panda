package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"redpanda/gateway/internal/gateway/service"
)

type AgentDefinitionController struct {
	Services service.Set
}

type agentDefinitionCreateRequest struct {
	Key             string   `json:"key"`
	Name            string   `json:"name"`
	NameZH          string   `json:"name_zh"`
	Phase           string   `json:"phase"`
	Description     string   `json:"description"`
	SystemPrompt    string   `json:"system_prompt"`
	ToolAllowlist   []string `json:"tool_allowlist"`
	ToolDenylist    []string `json:"tool_denylist"`
	DefaultMaxTurns int      `json:"default_max_turns"`
	Enabled         *bool    `json:"enabled"`
	SortOrder       int      `json:"sort_order"`
}

type agentDefinitionUpdateRequest struct {
	Name            *string   `json:"name"`
	NameZH          *string   `json:"name_zh"`
	Phase           *string   `json:"phase"`
	Description     *string   `json:"description"`
	SystemPrompt    *string   `json:"system_prompt"`
	ToolAllowlist   *[]string `json:"tool_allowlist"`
	ToolDenylist    *[]string `json:"tool_denylist"`
	DefaultMaxTurns *int      `json:"default_max_turns"`
	Enabled         *bool     `json:"enabled"`
	SortOrder       *int      `json:"sort_order"`
}

func (a AgentDefinitionController) List(c *gin.Context) {
	items, err := a.Services.Agents.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "agent_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (a AgentDefinitionController) ListEnabled(c *gin.Context) {
	items, err := a.Services.Agents.ListEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{"code": "agent_list_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (a AgentDefinitionController) Get(c *gin.Context) {
	item, err := a.Services.Agents.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": gin.H{"code": "agent_not_found", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (a AgentDefinitionController) Create(c *gin.Context) {
	var req agentDefinitionCreateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid agent payload"}})
		return
	}
	item, err := a.Services.Agents.Create(service.AgentDefinitionCreate{
		Key:             req.Key,
		Name:            req.Name,
		NameZH:          req.NameZH,
		Phase:           req.Phase,
		Description:     req.Description,
		SystemPrompt:    req.SystemPrompt,
		ToolAllowlist:   req.ToolAllowlist,
		ToolDenylist:    req.ToolDenylist,
		DefaultMaxTurns: req.DefaultMaxTurns,
		Enabled:         req.Enabled,
		SortOrder:       req.SortOrder,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "agent_create_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (a AgentDefinitionController) Update(c *gin.Context) {
	var req agentDefinitionUpdateRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid agent payload"}})
		return
	}
	item, err := a.Services.Agents.Update(c.Param("id"), service.AgentDefinitionUpdate{
		Name:            req.Name,
		NameZH:          req.NameZH,
		Phase:           req.Phase,
		Description:     req.Description,
		SystemPrompt:    req.SystemPrompt,
		ToolAllowlist:   req.ToolAllowlist,
		ToolDenylist:    req.ToolDenylist,
		DefaultMaxTurns: req.DefaultMaxTurns,
		Enabled:         req.Enabled,
		SortOrder:       req.SortOrder,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "agent_update_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (a AgentDefinitionController) Delete(c *gin.Context) {
	if err := a.Services.Agents.Delete(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "agent_delete_failed", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, envelope(c, gin.H{"deleted": true, "id": c.Param("id")}))
}
