package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

// TimeoutClass 工具超时分类
type TimeoutClass uint8

const (
	LocalToolTimeout TimeoutClass = iota // 30s
	GatewayToolTimeout                   // 30s
	SelfManagedToolTimeout               // 0 (无限)
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
	OpsOnly      bool   // 替代旧 policy.go 的 opsOnlyTools
	Source       string // "builtin:workspace" | "js:xxx" | "mcp:server"
	Overriden    string // 若非空，表示覆盖了同名的旧来源
}

// Registry 工具注册表
type Registry struct {
	mu      sync.RWMutex
	entries map[string]ToolEntry // 按 name 索引
	order   []string             // 注册顺序（公开顺序）
	bySrc   map[string][]string  // Source prefix → []name
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]ToolEntry),
		bySrc:   make(map[string][]string),
	}
}

// Register 注册工具。同名时覆盖旧 entry：旧 entry 的 Overriden 清空，新 entry 的
// Overriden 设为旧 Source；order 数组 append（不重复）；bySrc 更新。
// 返回被覆盖的旧 entry，若无覆盖则返回 nil。
func (r *Registry) Register(e ToolEntry) *ToolEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	prev, existed := r.entries[e.Definition.Name]
	if existed {
		prev.Overriden = ""
		e.Overriden = prev.Source
	}

	r.entries[e.Definition.Name] = e

	// order: append if not already present.
	if _, exists := r.findInOrder(e.Definition.Name); !exists {
		r.order = append(r.order, e.Definition.Name)
	}

	// bySrc: keep per-source name lists consistent.
	r.rebuildBySrc()

	if existed {
		return &prev
	}
	return nil
}

// Definitions 按注册顺序返回工具定义列表。
func (r *Registry) Definitions() []ptools.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	outs := make([]ptools.Definition, 0, len(r.order))
	for _, name := range r.order {
		if e, ok := r.entries[name]; ok {
			out := e.Definition
			outs = append(outs, out)
		}
	}
	return outs
}

// Lookup 按名称查找工具 entry，返回 entry 指针与存在标志。
func (r *Registry) Lookup(name string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entries[name]
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
		if e, ok := r.entries[name]; ok && strings.HasPrefix(e.Source, prefix) {
			names = append(names, name)
		}
	}
	return names
}

// Clear 删除 Source 以 prefix 开头的所有工具（前缀匹配）。
func (r *Registry) Clear(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var newOrder []string
	for _, name := range r.order {
		if e, ok := r.entries[name]; ok && !strings.HasPrefix(e.Source, prefix) {
			newOrder = append(newOrder, name)
		}
	}
	for _, name := range r.order {
		if e, ok := r.entries[name]; ok && strings.HasPrefix(e.Source, prefix) {
			delete(r.entries, name)
		}
	}
	r.order = newOrder
	r.rebuildBySrc()
}

// Remove 精确删除指定名称的工具。
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries[name]; !ok {
		return
	}
	delete(r.entries, name)

	// 从 order 中移除
	newOrder := make([]string, 0, len(r.order))
	for _, n := range r.order {
		if n != name {
			newOrder = append(newOrder, n)
		}
	}
	r.order = newOrder
	r.rebuildBySrc()
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
		if e, ok := r.entries[name]; ok {
			r.bySrc[e.Source] = append(r.bySrc[e.Source], name)
		}
	}
}

// Names 返回当前注册的所有工具名称（注册顺序）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
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
