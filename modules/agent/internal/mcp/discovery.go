package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"redpanda/mcpkit"
	protomcp "redpanda/protocol/mcp"
)

// DiscoverServer 发现 MCP 工具列表。优先使用 Manager 级缓存；未命中时复用
// 长连接会话（initialize 一次，后续 tools/list 与 tools/call 共享进程）。
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
	if m != nil && m.isDisabled(config.Name) {
		result.Error = disabledServerError(config.Name).Error()
		return result
	}

	key := configIdentity(workspaceRoot, config)
	if cached, ok := m.getCachedDiscovery(key); ok {
		cached.Name = config.Name
		return filterDiscoveryAllowlist(cached, config)
	}

	timeouts := config.Timeouts.Normalized()
	startedAt := time.Now()
	var listed []mcpkit.ToolDefinition
	var serverName, serverVersion string
	var stderr string

	err := m.withSession(ctx, workspaceRoot, config, timeouts.StartMS, timeouts.InitializeMS, func(session *mcpkit.Session, entry *liveEntry) error {
		listCtx := ctx
		var listCancel context.CancelFunc
		if timeouts.ListMS > 0 {
			listCtx, listCancel = context.WithTimeout(ctx, time.Duration(timeouts.ListMS)*time.Millisecond)
			defer listCancel()
		}
		tools, listErr := session.ListTools(listCtx)
		if listErr != nil {
			return fmt.Errorf("tools/list failed: %s", formatInitError(listErr))
		}
		listed = tools
		stderr = session.StderrSummary()
		serverName = entry.serverName
		serverVersion = entry.serverVer
		return nil
	})

	result.DurationMS = time.Since(startedAt).Milliseconds()
	result.StderrSummary = mcpkit.RedactSecrets(stderr, config.Env)
	if err != nil {
		result.Error = mcpkit.RedactSecrets(err.Error(), config.Env)
		return result
	}

	result.Status = "ready"
	result.ServerInfo = protomcp.MCPServerInfo{Name: serverName, Version: serverVersion}
	for _, tool := range listed {
		result.Tools = append(result.Tools, protomcp.MCPToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	m.putCachedDiscovery(key, result)
	return filterDiscoveryAllowlist(result, config)
}

func filterDiscoveryAllowlist(result protomcp.MCPServerDiscovery, config protomcp.MCPServerConfig) protomcp.MCPServerDiscovery {
	allowedTools := stringSet(config.ToolAllowlist)
	if len(allowedTools) == 0 {
		return result
	}
	filtered := result
	filtered.Tools = make([]protomcp.MCPToolDefinition, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if _, allowed := allowedTools[tool.Name]; allowed {
			filtered.Tools = append(filtered.Tools, tool)
		}
	}
	return filtered
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
