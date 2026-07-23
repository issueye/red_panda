package mcp

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

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

	// sessions reuses one stdio process per server config identity across
	// tools/list and tools/call (docs/19 process reuse backlog).
	sessions map[string]*liveEntry
	// discovery caches tools/list results for the Manager lifetime when the
	// server config identity is unchanged (cross-run discovery cache).
	discovery map[string]discoveryCacheEntry

	// failures tracks startup-phase failures per server Name (docs/19 §7.7).
	// Lifetime is the Manager (= Runtime process); state is shared across runs
	// so a server that crashes in one run stays disabled in the next. Keyed by
	// MCPServerConfig.Name because Runtime does not receive the Gateway DB id.
	failures    map[string]*serverFailure
	crashLimit  int
	crashWindow time.Duration
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
		version:     version,
		log:         log,
		tracker:     mcpkit.NewTracker(),
		bindings:    map[string]map[string]ToolBinding{},
		sessions:    map[string]*liveEntry{},
		discovery:   map[string]discoveryCacheEntry{},
		failures:    map[string]*serverFailure{},
		crashLimit:  crashLimitFromEnv(),
		crashWindow: crashWindowFromEnv(),
	}
}

// CloseAll 关闭所有复用中的 MCP 会话与 Tracker（Runtime core.shutdown）。
func (m *Manager) CloseAll() {
	m.closeAllSessions()
}

// isDisabled reports whether serverName has been auto-disabled by the crash
// budget (docs/19 §7.7). Caller may hold mu or not; this function locks it.
func (m *Manager) isDisabled(serverName string) bool {
	if m.crashLimit <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.failures[serverName]
	return ok && f.disabled
}

// recordStartupFailure records a startup-phase failure for serverName and
// returns whether the budget has now disabled the server. The caller must
// have already classified the error via isStartupPhaseFailure. Returns false
// immediately when the budget is disabled (crashLimit <= 0) or the server was
// already disabled (idempotent no-op).
func (m *Manager) recordStartupFailure(serverName string) bool {
	if m.crashLimit <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.failures[serverName]
	if f == nil {
		f = &serverFailure{}
		m.failures[serverName] = f
	}
	if f.disabled {
		return true
	}
	f.recent, f.disabled = failureDecision(f.recent, time.Now(), m.crashWindow, m.crashLimit)
	return f.disabled
}

// DisabledServers returns the sorted Names of servers currently auto-disabled
// by the crash budget. Used for diagnostics and by PrepareToolsForRun callers
// that want to log skip reasons. Returns nil when the budget feature is off.
func (m *Manager) DisabledServers() []string {
	if m.crashLimit <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.failures))
	for name, f := range m.failures {
		if f.disabled {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// disabledServerError returns the user-visible error string for an ExecuteTool
// call against an auto-disabled server.
func disabledServerError(serverName string) error {
	return fmt.Errorf("MCP server %s disabled by crash budget (too many startup failures)", serverName)
}
