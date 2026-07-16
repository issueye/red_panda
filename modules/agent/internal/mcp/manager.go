package mcp

import (
	"io"
	"sync"

	"redpanda/mcpkit"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/tools"
)

// Manager 负责 MCP 进程生命周期、工具发现和 tools/call（文档 36 D2）。
// 底层协议与 stdio 传输由 redpanda/mcpkit（mark3labs/mcp-go）封装。
type Manager struct {
	version string
	log     io.Writer

	tracker *mcpkit.Tracker

	mu sync.Mutex
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
		version:  version,
		log:      log,
		tracker:  mcpkit.NewTracker(),
		bindings: map[string]map[string]ToolBinding{},
	}
}

// CloseAll 关闭所有由 Manager 跟踪的 MCP 会话（Runtime core.shutdown）。
func (m *Manager) CloseAll() {
	if m.tracker != nil {
		m.tracker.CloseAll()
	}
}
