package mcp

import (
	"io"
	"sync"

	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/tools"
)

// Manager 负责 MCP 进程生命周期、工具发现和 tools/call（文档 36 D2）。
type Manager struct {
	version string
	log     io.Writer

	mu        sync.Mutex
	processes map[*mcpProcess]struct{}
	// 绑定关系：runID -> 规范名称 -> 绑定信息。
	bindings map[string]map[string]ToolBinding
}

// ToolBinding 将面向提供方的规范名称映射到服务端及原始工具。
type ToolBinding struct {
	Config  protomcp.MCPServerConfig
	RawName string
	Def     tools.Definition
}

// NewManager 为 Runtime 进程创建 MCP 管理器。
func NewManager(version string, log io.Writer) *Manager {
	if log == nil {
		log = io.Discard
	}
	return &Manager{
		version:   version,
		log:       log,
		processes: map[*mcpProcess]struct{}{},
		bindings:  map[string]map[string]ToolBinding{},
	}
}
