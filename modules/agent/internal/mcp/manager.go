package mcp

import (
	"io"
	"sync"

	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/tools"
)

// Manager owns MCP process lifecycle, discovery, and tools/call (docs/36 D2).
type Manager struct {
	version string
	log     io.Writer

	mu        sync.Mutex
	processes map[*mcpProcess]struct{}
	// bindings: runID -> canonical name -> binding
	bindings map[string]map[string]ToolBinding
}

// ToolBinding maps a provider-facing canonical name to a server + raw tool.
type ToolBinding struct {
	Config  protomcp.MCPServerConfig
	RawName string
	Def     tools.Definition
}

// NewManager constructs an MCP manager for a Runtime process.
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
