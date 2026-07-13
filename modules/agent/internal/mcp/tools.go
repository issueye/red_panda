package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

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

// callMCPTool 启动一次性 MCP stdio 会话，执行 tools/call 后清理资源。
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
	if strings.TrimSpace(rawToolName) == "" {
		return "", fmt.Errorf("MCP tool name is required")
	}
	timeouts := config.Timeouts.Normalized()
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Env = append(os.Environ(), envPairs(config.Env)...)
	if config.CWD != "" {
		cmd.Dir = config.CWD
		if !filepath.IsAbs(cmd.Dir) && workspaceRoot != "" {
			cmd.Dir = filepath.Join(workspaceRoot, config.CWD)
		}
	} else if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("start failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return "", fmt.Errorf("start failed: %w", err)
	}
	stderr := &boundedBuffer{max: mcpStderrLimit}
	cmd.Stderr = stderr
	process := &mcpProcess{
		cmd:             cmd,
		stdin:           stdin,
		done:            make(chan struct{}),
		startDone:       make(chan struct{}),
		shutdownTimeout: durationMillis(timeouts.ShutdownMS),
	}
	m.registerProcess(process)
	defer func() {
		process.close(durationMillis(timeouts.ShutdownMS))
		m.unregisterProcess(process)
		if err != nil {
			msg := redactMCPSecrets(err.Error(), config.Env)
			if summary := stderr.String(); summary != "" {
				msg = msg + " (stderr: " + redactMCPSecrets(summary, config.Env) + ")"
			}
			err = fmt.Errorf("%s", msg)
		}
	}()

	startResult := make(chan error, 1)
	go func() { startResult <- process.start() }()
	select {
	case err = <-startResult:
		if err != nil {
			return "", fmt.Errorf("start failed: %w", err)
		}
	case <-time.After(durationMillis(timeouts.StartMS)):
		return "", fmt.Errorf("start timeout after %dms", timeouts.StartMS)
	case <-ctx.Done():
		return "", fmt.Errorf("start failed: %w", ctx.Err())
	}

	lines := make(chan []byte)
	readErrors := make(chan error, 1)
	stopReader := make(chan struct{})
	defer close(stopReader)
	go readMCPLines(stdout, lines, readErrors, stopReader)

	initialize := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "red-panda-agent", "version": m.version},
		},
	}
	if err = writeMCPMessage(stdin, initialize); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}
	var initResult map[string]any
	if err = waitMCPResponse(ctx, lines, readErrors, process, 1, durationMillis(timeouts.InitializeMS), &initResult); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}
	if err = writeMCPMessage(stdin, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}

	if arguments == nil {
		arguments = map[string]any{}
	}
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      rawToolName,
			"arguments": arguments,
		},
	}
	if err = writeMCPMessage(stdin, callReq); err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err = waitMCPResponse(ctx, lines, readErrors, process, 3, durationMillis(timeouts.CallMS), &callResult); err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}
	var parts []string
	for _, block := range callResult.Content {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	text := strings.Join(parts, "\n")
	if callResult.IsError {
		if text == "" {
			text = "MCP tool returned isError"
		}
		return text, fmt.Errorf("%s", text)
	}
	if text == "" {
		// 为活动记录和 UI 保留结构化的空成功结果。
		raw, _ := json.Marshal(callResult)
		text = string(raw)
	}
	if len(text) > maxToolOutputBytes {
		text = text[:maxToolOutputBytes] + "\n…(truncated)"
	}
	return text, nil
}

// mcpCanonicalName 构建 mcp__server__tool 名称（设计文档 19，第 8.1 节）。
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
