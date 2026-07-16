package mcp

import (
	"context"
	"strings"

	"redpanda/mcpkit"
	protomcp "redpanda/protocol/mcp"
)

// DiscoverServer 启动一次性 MCP stdio 会话，执行 initialize + tools/list 后清理。
func (m *Manager) DiscoverServer(ctx context.Context, workspaceRoot string, config protomcp.MCPServerConfig) (result protomcp.MCPServerDiscovery) {
	result.Name = config.Name
	result.Status = "failed"
	result.Tools = []protomcp.MCPToolDefinition{}

	if !config.Enabled {
		result.Status = "disabled"
		return result
	}
	if strings.TrimSpace(config.Command) == "" {
		result.Error = "start failed: command is required"
		return result
	}

	timeouts := config.Timeouts.Normalized()
	discovery := mcpkit.Discover(ctx, sessionConfig(m, workspaceRoot, config), mcpkit.DiscoverOptions{
		StartMS:      timeouts.StartMS,
		InitializeMS: timeouts.InitializeMS,
		ListMS:       timeouts.ListMS,
		ShutdownMS:   timeouts.ShutdownMS,
		Tracker:      m.tracker,
	})

	result.Status = discovery.Status
	result.Error = discovery.Error
	result.StderrSummary = discovery.StderrSummary
	result.DurationMS = discovery.DurationMS
	result.ServerInfo = protomcp.MCPServerInfo{
		Name:    discovery.ServerInfo.Name,
		Version: discovery.ServerInfo.Version,
	}

	allowedTools := stringSet(config.ToolAllowlist)
	for _, tool := range discovery.Tools {
		if len(allowedTools) > 0 {
			if _, allowed := allowedTools[tool.Name]; !allowed {
				continue
			}
		}
		result.Tools = append(result.Tools, protomcp.MCPToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return result
}

func sessionConfig(m *Manager, workspaceRoot string, config protomcp.MCPServerConfig) mcpkit.SessionConfig {
	version := "0.0.0"
	if m != nil && m.version != "" {
		version = m.version
	}
	return mcpkit.SessionConfig{
		Command:         config.Command,
		Args:            config.Args,
		Env:             config.Env,
		Dir:             config.CWD,
		WorkspaceRoot:   workspaceRoot,
		ClientName:      "red-panda-agent",
		ClientVersion:   version,
		ProtocolVersion: mcpkit.DefaultProtocolVersion,
	}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
