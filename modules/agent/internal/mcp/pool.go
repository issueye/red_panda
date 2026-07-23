package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"redpanda/mcpkit"
	protomcp "redpanda/protocol/mcp"
)

// liveEntry holds one long-lived MCP stdio session for a server config identity.
// MCP stdio is single-flight per process; mu serializes tools/call and list.
type liveEntry struct {
	mu         sync.Mutex
	session    *mcpkit.Session
	cfg        protomcp.MCPServerConfig
	ws         string
	serverName string
	serverVer  string
}

type discoveryCacheEntry struct {
	key      string
	result   protomcp.MCPServerDiscovery
	storedAt time.Time
}

func (m *Manager) ensurePool() {
	if m.sessions == nil {
		m.sessions = map[string]*liveEntry{}
	}
	if m.discovery == nil {
		m.discovery = map[string]discoveryCacheEntry{}
	}
}

// configIdentity is a stable key for process reuse and discovery cache.
// Workspace is included so relative CWD resolution stays correct.
func configIdentity(workspaceRoot string, config protomcp.MCPServerConfig) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(config.Name))
	b.WriteByte('\x00')
	b.WriteString(strings.TrimSpace(config.Command))
	b.WriteByte('\x00')
	for _, arg := range config.Args {
		b.WriteString(arg)
		b.WriteByte('\x00')
	}
	b.WriteString(strings.TrimSpace(config.CWD))
	b.WriteByte('\x00')
	b.WriteString(strings.TrimSpace(workspaceRoot))
	b.WriteByte('\x00')
	if len(config.Env) > 0 {
		keys := make([]string, 0, len(config.Env))
		for k := range config.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte('=')
			b.WriteString(config.Env[k])
			b.WriteByte('\x00')
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// withSession runs fn against a live, initialized MCP session for config.
// Sessions are reused across calls; transport failures drop the session so the
// next call can recover. fn runs under the per-server mutex.
func (m *Manager) withSession(
	ctx context.Context,
	workspaceRoot string,
	config protomcp.MCPServerConfig,
	startMS, initializeMS int,
	fn func(session *mcpkit.Session, entry *liveEntry) error,
) error {
	if m == nil {
		return fmt.Errorf("MCP manager is nil")
	}
	key := configIdentity(workspaceRoot, config)

	m.mu.Lock()
	m.ensurePool()
	entry := m.sessions[key]
	if entry == nil {
		entry = &liveEntry{cfg: config, ws: workspaceRoot}
		m.sessions[key] = entry
	}
	m.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	session, err := m.openLiveSession(ctx, entry, workspaceRoot, config, startMS, initializeMS)
	if err != nil {
		return err
	}
	if err := fn(session, entry); err != nil {
		if isSessionTransportFailure(err) {
			m.dropLiveSessionLocked(entry)
		}
		return err
	}
	return nil
}

func (m *Manager) openLiveSession(
	ctx context.Context,
	entry *liveEntry,
	workspaceRoot string,
	config protomcp.MCPServerConfig,
	startMS, initializeMS int,
) (*mcpkit.Session, error) {
	if entry.session != nil && !entry.session.Closed() {
		return entry.session, nil
	}
	if entry.session != nil {
		m.dropLiveSessionLocked(entry)
	}

	startCtx := ctx
	var startCancel context.CancelFunc
	if startMS > 0 {
		startCtx, startCancel = context.WithTimeout(ctx, time.Duration(startMS)*time.Millisecond)
		defer startCancel()
	}
	session, err := mcpkit.Open(startCtx, sessionConfig(m, workspaceRoot, config))
	if err != nil {
		msg := err.Error()
		if isTimeoutErr(err) && startMS > 0 {
			msg = fmt.Sprintf("start timeout after %dms", startMS)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if m.tracker != nil {
		m.tracker.Track(session)
	}

	initCtx := ctx
	var initCancel context.CancelFunc
	if initializeMS > 0 {
		initCtx, initCancel = context.WithTimeout(ctx, time.Duration(initializeMS)*time.Millisecond)
		defer initCancel()
	}
	info, err := session.Initialize(initCtx)
	if err != nil {
		if m.tracker != nil {
			m.tracker.Untrack(session)
		}
		_ = session.Close()
		return nil, fmt.Errorf("initialize failed: %s", formatInitError(err))
	}
	entry.session = session
	entry.cfg = config
	entry.ws = workspaceRoot
	entry.serverName = info.Name
	entry.serverVer = info.Version
	return session, nil
}

func (m *Manager) dropLiveSessionLocked(entry *liveEntry) {
	if entry == nil || entry.session == nil {
		return
	}
	if m.tracker != nil {
		m.tracker.Untrack(entry.session)
	}
	_ = entry.session.Close()
	entry.session = nil
	entry.serverName = ""
	entry.serverVer = ""
}

// LiveSessionCount returns the number of open pooled MCP processes (tests / diagnostics).
func (m *Manager) LiveSessionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, entry := range m.sessions {
		if entry != nil && entry.session != nil && !entry.session.Closed() {
			n++
		}
	}
	return n
}

// closeAllSessions closes every pooled MCP process (Runtime shutdown).
func (m *Manager) closeAllSessions() {
	m.mu.Lock()
	entries := make([]*liveEntry, 0, len(m.sessions))
	for _, entry := range m.sessions {
		entries = append(entries, entry)
	}
	m.sessions = map[string]*liveEntry{}
	m.discovery = map[string]discoveryCacheEntry{}
	m.mu.Unlock()

	for _, entry := range entries {
		entry.mu.Lock()
		m.dropLiveSessionLocked(entry)
		entry.mu.Unlock()
	}
	if m.tracker != nil {
		m.tracker.CloseAll()
	}
}

func (m *Manager) getCachedDiscovery(key string) (protomcp.MCPServerDiscovery, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensurePool()
	entry, ok := m.discovery[key]
	if !ok {
		return protomcp.MCPServerDiscovery{}, false
	}
	// Cache lives for Manager lifetime; config identity change uses a new key.
	out := entry.result
	out.Tools = append([]protomcp.MCPToolDefinition(nil), entry.result.Tools...)
	return out, true
}

func (m *Manager) putCachedDiscovery(key string, result protomcp.MCPServerDiscovery) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensurePool()
	cached := result
	cached.Tools = append([]protomcp.MCPToolDefinition(nil), result.Tools...)
	m.discovery[key] = discoveryCacheEntry{
		key:      key,
		result:   cached,
		storedAt: time.Now(),
	}
}

func (m *Manager) invalidateDiscovery(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.discovery != nil {
		delete(m.discovery, key)
	}
}

func isSessionTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Business isError / tool logic failures keep the process.
	if strings.HasPrefix(err.Error(), "tools/call failed:") &&
		!strings.Contains(msg, "timeout") &&
		!strings.Contains(msg, "process") &&
		!strings.Contains(msg, "eof") &&
		!strings.Contains(msg, "broken pipe") &&
		!strings.Contains(msg, "closed") {
		return false
	}
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "process exited") ||
		strings.Contains(msg, "transport closed") ||
		strings.Contains(msg, "file already closed") ||
		strings.Contains(msg, "initialize failed") ||
		strings.Contains(msg, "start timeout") ||
		strings.Contains(msg, "start failed")
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context deadline") ||
		strings.Contains(msg, "context canceled")
}

func formatInitError(err error) string {
	if err == nil {
		return ""
	}
	if isTimeoutErr(err) {
		return "timeout"
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	// Preserve historical Runtime wording expected by discovery tests / logs.
	if strings.Contains(lower, "transport closed") ||
		strings.Contains(lower, "eof") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "file already closed") {
		return "process exited: " + msg
	}
	return msg
}
