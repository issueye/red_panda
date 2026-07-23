package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/repository"
	"redpanda/gateway/internal/gateway/service"
	protocolmcp "redpanda/protocol/mcp"
)

type MCPServerConfigController struct {
	Services service.Set
}

type mcpServerUpdateRequest struct {
	Name          *string                  `json:"name"`
	Command       *string                  `json:"command"`
	Args          *[]string                `json:"args"`
	Env           *map[string]string       `json:"env"`
	CWD           *string                  `json:"cwd"`
	Enabled       *bool                    `json:"enabled"`
	Timeouts      *protocolmcp.MCPTimeouts `json:"timeouts"`
	ToolAllowlist *[]string                `json:"tool_allowlist"`
	RiskOverrides *map[string]string       `json:"risk_overrides"`
}

func (m MCPServerConfigController) List(c *gin.Context) {
	items, err := m.Services.MCPServers.List()
	if err != nil {
		writeMCPServerError(c, "list", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, items))
}

func (m MCPServerConfigController) Create(c *gin.Context) {
	var req protocolmcp.MCPServerCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid MCP server payload"}})
		return
	}
	item, err := m.Services.MCPServers.Create(req.MCPServerConfig)
	if err != nil {
		writeMCPServerError(c, "create", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MCPServerConfigController) Get(c *gin.Context) {
	item, err := m.Services.MCPServers.Get(c.Param("id"))
	if err != nil {
		writeMCPServerError(c, "get", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MCPServerConfigController) Update(c *gin.Context) {
	var req mcpServerUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid MCP server payload"}})
		return
	}
	item, err := m.Services.MCPServers.Update(c.Param("id"), service.MCPServerConfigUpdate{
		Name:          req.Name,
		Command:       req.Command,
		Args:          req.Args,
		Env:           req.Env,
		CWD:           req.CWD,
		Enabled:       req.Enabled,
		Timeouts:      req.Timeouts,
		ToolAllowlist: req.ToolAllowlist,
		RiskOverrides: req.RiskOverrides,
	})
	if err != nil {
		writeMCPServerError(c, "update", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, item))
}

func (m MCPServerConfigController) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := m.Services.MCPServers.Delete(id); err != nil {
		writeMCPServerError(c, "delete", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, protocolmcp.MCPServerDeleteResponse{ID: id, Deleted: true}))
}

func (m MCPServerConfigController) Discover(c *gin.Context) {
	var req struct {
		WorkspaceRoot string `json:"workspace_root"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid MCP discovery payload"}})
			return
		}
	}
	result, err := m.Services.MCPServers.Discover(c.Request.Context(), c.Param("id"), req.WorkspaceRoot)
	if err != nil {
		writeMCPServerError(c, "discover", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func (m MCPServerConfigController) Call(c *gin.Context) {
	var req struct {
		WorkspaceRoot string         `json:"workspace_root"`
		ToolName      string         `json:"tool_name"`
		Arguments     map[string]any `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": gin.H{"code": "invalid_payload", "message": "invalid MCP call payload"}})
		return
	}
	result, err := m.Services.MCPServers.CallTool(c.Request.Context(), c.Param("id"), req.WorkspaceRoot, req.ToolName, req.Arguments)
	if err != nil {
		writeMCPServerError(c, "call", err)
		return
	}
	c.JSON(http.StatusOK, envelope(c, result))
}

func writeMCPServerError(c *gin.Context, operation string, err error) {
	status := http.StatusInternalServerError
	code := "mcp_server_" + operation + "_failed"
	switch {
	case errors.Is(err, service.ErrInvalidMCPServerConfig):
		status = http.StatusBadRequest
		code = "invalid_mcp_server_config"
	case errors.Is(err, repository.ErrMCPServerNameExists):
		status = http.StatusConflict
		code = "mcp_server_name_exists"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status = http.StatusNotFound
		code = "mcp_server_not_found"
	}
	c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": code, "message": err.Error()}})
}
