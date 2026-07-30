package registry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"redpanda/protocol/methods"
	"redpanda/protocol/pluginmeta"
	ptools "redpanda/protocol/tools"
)

// TimeoutClass 工具超时分类
type TimeoutClass uint8

const (
	LocalToolTimeout       TimeoutClass = iota // 30s
	GatewayToolTimeout                         // 30s
	SelfManagedToolTimeout                     // 0 (无限)
)

// Duration 返回 TimeoutClass 对应的超时时长，0 表示无限。
func (c TimeoutClass) Duration() time.Duration {
	switch c {
	case GatewayToolTimeout:
		return 30 * time.Second
	case SelfManagedToolTimeout:
		return 0
	default:
		return 30 * time.Second
	}
}

// HandlerFunc 工具执行器签名
type HandlerFunc func(ctx context.Context, toolCtx *ToolContext, args map[string]any) (*ptools.Result, error)

// ToolContext 工具执行上下文（依赖注入）
type ToolContext struct {
	ToolCallID   string
	RunID        string
	SessionID    string
	AssignmentID string
	WorkerID     string
	WorkingDir   string
	Reply        *methods.ReplyParams
}

// ToolEntry 注册表中每个工具的描述
type ToolEntry struct {
	Definition   ptools.Definition // Name/DisplayName/Description/Risk/Parameters
	Handler      HandlerFunc       // 执行函数
	TimeoutClass TimeoutClass
	OpsOnly      bool   // 仅在显式开启调试工具时公开
	Source       string // "builtin:workspace" | "js:xxx" | "mcp:server"
	Overridden   string // 若非空，表示覆盖了同名的旧来源
}

// Registry 工具注册表
type Registry struct {
	mu      sync.RWMutex
	entries map[string][]ToolEntry // 按 name 保存覆盖栈，栈顶为当前 entry
	order   []string               // 首次注册顺序（公开顺序）
	bySrc   map[string][]string    // 当前 entry 的 Source → []name
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string][]ToolEntry),
		bySrc:   make(map[string][]string),
	}
}

// Clone returns an independent snapshot including override layers and public order.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clone := NewRegistry()
	clone.order = append([]string(nil), r.order...)
	for name, stack := range r.entries {
		clone.entries[name] = append([]ToolEntry(nil), stack...)
	}
	clone.rebuildBySrc()
	return clone
}

// RunScopedRegistry is an immutable-global snapshot plus entries discovered for one run.
// Dynamic entries can be registered on the embedded Registry without mutating the global registry.
type RunScopedRegistry struct {
	*Registry
}

func NewRunScopedRegistry(global *Registry) *RunScopedRegistry {
	if global == nil {
		global = NewRegistry()
	}
	return &RunScopedRegistry{Registry: global.Clone()}
}

// Register 注册工具。同名时后注册的来源覆盖先注册来源。相同来源重新注册会
// 替换自己的旧层，避免 reload 产生重复栈层。
// 返回被覆盖的旧 entry，若无覆盖则返回 nil。
func (r *Registry) Register(e ToolEntry) (*ToolEntry, error) {
	if err := pluginmeta.ValidateToolName(e.Definition.Name); err != nil {
		return nil, err
	}
	if err := pluginmeta.ValidateSource(e.Source); err != nil {
		return nil, err
	}
	if e.Handler == nil {
		return nil, fmt.Errorf("tool %q has no handler", e.Definition.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	name := e.Definition.Name
	stack := r.entries[name]
	var prev *ToolEntry
	if len(stack) > 0 {
		current := stack[len(stack)-1]
		current.Overridden = ""
		prev = &current
	}

	stack = removeSource(stack, e.Source)
	e.Overridden = ""
	if len(stack) > 0 {
		e.Overridden = stack[len(stack)-1].Source
	}
	r.entries[name] = append(stack, e)

	if _, exists := r.findInOrder(name); !exists {
		r.order = append(r.order, name)
	}

	r.rebuildBySrc()
	return prev, nil
}

// MustRegister registers a compile-time built-in entry and panics on an invalid definition.
func (r *Registry) MustRegister(e ToolEntry) *ToolEntry {
	previous, err := r.Register(e)
	if err != nil {
		panic(err)
	}
	return previous
}

// Definitions 按注册顺序返回工具定义列表。
func (r *Registry) Definitions() []ptools.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	outs := make([]ptools.Definition, 0, len(r.order))
	for _, name := range r.order {
		if e, ok := currentEntry(r.entries[name]); ok {
			out := e.Definition
			outs = append(outs, out)
		}
	}
	return outs
}

// FilterDefinitions applies per-run allow/deny and OpsOnly visibility using
// ToolEntry as the single metadata source.
func (r *Registry) FilterDefinitions(definitions []ptools.Definition, options methods.ReplyOptions) []ptools.Definition {
	filtered := make([]ptools.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if contains(options.ToolDenylist, definition.Name) {
			continue
		}
		if len(options.ToolAllowlist) > 0 && !contains(options.ToolAllowlist, definition.Name) {
			continue
		}
		entry, ok := r.Lookup(definition.Name)
		if ok && entry.OpsOnly && !opsToolEnabled(options, definition.Name) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}

// Lookup 按名称查找工具 entry，返回 entry 指针与存在标志。
func (r *Registry) Lookup(name string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := currentEntry(r.entries[name])
	if !ok {
		return nil, false
	}
	return &e, true
}

// ListBySource 按 Source 前缀过滤，按注册顺序返回工具名。
func (r *Registry) ListBySource(prefix string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.order))
	for _, name := range r.order {
		if e, ok := currentEntry(r.entries[name]); ok && strings.HasPrefix(e.Source, prefix) {
			names = append(names, name)
		}
	}
	return names
}

// ClearSource 卸载 Source 以 prefix 开头的所有注册层。若被卸载来源覆盖了
// 其他来源，先前的 entry 会自动恢复。
func (r *Registry) ClearSource(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	for _, name := range r.order {
		stack := r.entries[name]
		kept := stack[:0]
		for _, entry := range stack {
			if strings.HasPrefix(entry.Source, prefix) {
				removed++
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(r.entries, name)
			continue
		}
		r.entries[name] = normalizeOverrides(kept)
	}
	r.compactOrder()
	r.rebuildBySrc()
	return removed
}

// Remove 删除指定工具名中属于 source 的注册层。
func (r *Registry) Remove(name, source string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	stack, ok := r.entries[name]
	if !ok {
		return false
	}
	kept := stack[:0]
	removed := false
	for _, entry := range stack {
		if entry.Source == source {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		delete(r.entries, name)
	} else {
		r.entries[name] = normalizeOverrides(kept)
	}
	r.compactOrder()
	r.rebuildBySrc()
	return true
}

// orderIndex 返回 name 在 order 中的索引与存在标志。
func (r *Registry) findInOrder(name string) (int, bool) {
	for i, n := range r.order {
		if n == name {
			return i, true
		}
	}
	return -1, false
}

// rebuildBySrc 根据当前 entries 和 order 重建 bySrc 索引。
func (r *Registry) rebuildBySrc() {
	r.bySrc = make(map[string][]string)
	for _, name := range r.order {
		if e, ok := currentEntry(r.entries[name]); ok {
			r.bySrc[e.Source] = append(r.bySrc[e.Source], name)
		}
	}
}

func (r *Registry) compactOrder() {
	order := r.order[:0]
	for _, name := range r.order {
		if len(r.entries[name]) > 0 {
			order = append(order, name)
		}
	}
	r.order = order
}

func currentEntry(stack []ToolEntry) (ToolEntry, bool) {
	if len(stack) == 0 {
		return ToolEntry{}, false
	}
	return stack[len(stack)-1], true
}

func removeSource(stack []ToolEntry, source string) []ToolEntry {
	kept := make([]ToolEntry, 0, len(stack))
	for _, entry := range stack {
		if entry.Source != source {
			kept = append(kept, entry)
		}
	}
	return normalizeOverrides(kept)
}

func normalizeOverrides(stack []ToolEntry) []ToolEntry {
	for i := range stack {
		stack[i].Overridden = ""
		if i > 0 {
			stack[i].Overridden = stack[i-1].Source
		}
	}
	return stack
}

func opsToolEnabled(options methods.ReplyOptions, name string) bool {
	if options.DebugTools || contains(options.ToolAllowlist, name) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_DEBUG_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// Names 返回当前注册的所有工具名称（注册顺序）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// Entries returns the active entry for each public tool in registration order.
func (r *Registry) Entries() []ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]ToolEntry, 0, len(r.order))
	for _, name := range r.order {
		if entry, ok := currentEntry(r.entries[name]); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// Execute resolves a tool by name and calls its handler with timeout enforcement.
// Returns the handler's Result and error (which may be nil on success).
// The caller is responsible for policy checks and permission handling before
// calling Execute.
func (r *Registry) Execute(ctx context.Context, name string, tc *ToolContext, args map[string]any) (*ptools.Result, error) {
	entry, ok := r.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %s", name)
	}
	if entry.Handler == nil {
		return nil, fmt.Errorf("tool %s has no handler", name)
	}

	// Timeout enforcement (only for non-SelfManaged timeouts)
	timeout := entry.TimeoutClass.Duration()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	return entry.Handler(ctx, tc, args)
}
