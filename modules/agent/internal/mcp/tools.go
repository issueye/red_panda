package mcp

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"redpanda/mcpkit"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

const maxToolOutputBytes = 64 * 1024

// PrepareToolsForRun 根据回复选项中启用的 MCP 服务配置发现工具，
// 并注册 tools/call 所需的绑定关系（文档 36 D2 / 文档 19）。
// 单个服务发现失败时仅忽略该服务的工具，Runtime 仍会继续运行。
func (m *Manager) PrepareToolsForRun(ctx context.Context, params methods.ReplyParams) []tools.Definition {
	servers := params.Options.MCPServers
	if len(servers) == 0 {
		return nil
	}
	workspace := params.Session.WorkingDir
	bindings := make(map[string]ToolBinding)
	defs := make([]tools.Definition, 0)
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		if strings.TrimSpace(server.Command) == "" || strings.TrimSpace(server.Name) == "" {
			continue
		}
		discovery := m.DiscoverServer(ctx, workspace, server)
		if discovery.Status != "ready" {
			fmt.Fprintf(m.log, "mcp discover %s: %s (%s)\n", server.Name, discovery.Status, discovery.Error)
			continue
		}
		for _, tool := range discovery.Tools {
			canonical := CanonicalName(server.Name, tool.Name)
			if canonical == "" {
				continue
			}
			if _, exists := bindings[canonical]; exists {
				// 优先保留首个注册的服务和工具组合，跳过重复项。
				continue
			}
			def := tools.Definition{
				Name:        canonical,
				DisplayName: "MCP " + server.Name + " / " + tool.Name,
				Description: strings.TrimSpace(tool.Description),
				Risk:        toolRisk(server, tool.Name),
				Parameters:  parametersSchema(tool.InputSchema),
			}
			if def.Description == "" {
				def.Description = "MCP tool " + tool.Name + " on server " + server.Name
			}
			bindings[canonical] = ToolBinding{Config: server, RawName: tool.Name, Def: def}
			defs = append(defs, def)
		}
	}
	m.setBindings(params.RunID, bindings)
	return defs
}

func (m *Manager) setBindings(runID string, bindings map[string]ToolBinding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bindings == nil {
		m.bindings = map[string]map[string]ToolBinding{}
	}
	if len(bindings) == 0 {
		delete(m.bindings, runID)
		return
	}
	m.bindings[runID] = bindings
}

func (m *Manager) ClearBindings(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bindings, runID)
}

func (m *Manager) Binding(runID, canonical string) (ToolBinding, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byRun := m.bindings[runID]
	if byRun == nil {
		return ToolBinding{}, false
	}
	b, ok := byRun[canonical]
	return b, ok
}

func (m *Manager) DefinitionsForRun(runID string) []tools.Definition {
	m.mu.Lock()
	defer m.mu.Unlock()
	byRun := m.bindings[runID]
	if len(byRun) == 0 {
		return nil
	}
	out := make([]tools.Definition, 0, len(byRun))
	for _, b := range byRun {
		out = append(out, b.Def)
	}
	return out
}

func (m *Manager) ExecuteTool(ctx context.Context, runID string, workingDir string, call tools.Call) (string, error) {
	binding, ok := m.Binding(runID, call.Name)
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %s", call.Name)
	}
	return m.CallTool(ctx, workingDir, binding.Config, binding.RawName, call.Arguments)
}

// CallTool 启动一次性 MCP stdio 会话，执行 tools/call 后清理资源。
// 进程复用延后至 D3 实现。
func (m *Manager) CallTool(
	ctx context.Context,
	workspaceRoot string,
	config protomcp.MCPServerConfig,
	rawToolName string,
	arguments map[string]any,
) (output string, err error) {
	if !config.Enabled {
		return "", fmt.Errorf("MCP server %s is disabled", config.Name)
	}
	if strings.TrimSpace(config.Command) == "" {
		return "", fmt.Errorf("MCP server %s has empty command", config.Name)
	}
	timeouts := config.Timeouts.Normalized()
	return mcpkit.Call(ctx, sessionConfig(m, workspaceRoot, config), rawToolName, arguments, mcpkit.CallOptions{
		StartMS:      timeouts.StartMS,
		InitializeMS: timeouts.InitializeMS,
		CallMS:       timeouts.CallMS,
		ShutdownMS:   timeouts.ShutdownMS,
		MaxOutput:    maxToolOutputBytes,
		Tracker:      m.tracker,
	})
}

// CanonicalName 构建 mcp__server__tool 名称（设计文档 19，第 8.1 节）。
func CanonicalName(serverName, rawTool string) string {
	server := strings.TrimSpace(serverName)
	raw := strings.TrimSpace(rawTool)
	if server == "" || raw == "" {
		return ""
	}
	return "mcp__" + server + "__" + sanitizeMCPToken(raw)
}

func sanitizeMCPToken(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '.' || r == '/' || r == '\\' || r == ':':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	for strings.Contains(out, "__") {
		// 双下划线仅作为 MCP 名称分隔符使用，令牌内部的连续下划线需合并。
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}

func toolRisk(config protomcp.MCPServerConfig, rawTool string) tools.Risk {
	if config.RiskOverrides != nil {
		if risk, ok := config.RiskOverrides[rawTool]; ok {
			switch strings.ToLower(strings.TrimSpace(risk)) {
			case "low":
				return tools.RiskLow
			case "medium":
				return tools.RiskMedium
			case "high":
				return tools.RiskHigh
			}
		}
	}
	// 在建立可信元数据约定前，默认风险等级为高（文档 19）。
	return tools.RiskHigh
}

func parametersSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	// 浅拷贝，防止调用方修改发现缓存。
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

func IsToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}
