package hooks

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"redpanda/protocol/pluginmeta"
)

// HookName 钩子事件名
type HookName string

const (
	HookRunStart              HookName = "run.start"
	HookRunEnd                HookName = "run.end"
	HookBeforeProviderRequest HookName = "provider.request_before"
	HookAfterProviderResponse HookName = "provider.response_after"
	HookToolCall              HookName = "tool.call"
	HookToolResult            HookName = "tool.result"
	HookPermissionCheck       HookName = "permission.check"
	HookSessionBeforeCompact  HookName = "session.before_compact"
	HookSessionAfterCompact   HookName = "session.after_compact"
	HookContextCompose        HookName = "context.compose"
	HookMessageEnd            HookName = "message.end"
	HookProjectTrust          HookName = "ext.project_trust"
)

// HookOrder 钩子介入顺序（数值小先执行）
type HookOrder uint16

const (
	HookOrderSystem HookOrder = 0   // 系统内置钩子（权限门、审计）
	HookOrderPlugin HookOrder = 100 // 用户插件
)

// HookContext 钩子执行上下文
type HookContext struct {
	Context   context.Context
	RunID     string
	SessionID string
	MessageID string
	CWD       string
	Mode      string
	HasUI     bool
	IsIdle    func() bool
	Abort     func() error
}

// HookResult 钩子处理器返回值
type HookResult struct {
	Block     bool
	Reason    string
	Cancel    bool
	Transform map[string]any
}

// HookDiagnostic records a recovered handler failure without failing the chain.
type HookDiagnostic struct {
	Source string
	Order  HookOrder
	Panic  string
}

// HookOutcome is the fully materialized result of a hook chain.
type HookOutcome struct {
	Event       map[string]any
	Blocked     bool
	Cancelled   bool
	Reason      string
	Diagnostics []HookDiagnostic
}

// HookHandler 钩子处理器
type HookHandler struct {
	Order   HookOrder
	Handler func(ctx *HookContext, event map[string]any) *HookResult
	Source  string // "builtin:<domain>" | "js:<plugin-id>" | "mcp:<server-id>"
}

// ExtensionBus 扩展总线
type ExtensionBus struct {
	mu    sync.RWMutex
	hooks map[HookName][]*HookHandler // 按 order 升序排列
}

// NewBus 创建空的扩展总线
func NewBus() *ExtensionBus {
	return &ExtensionBus{
		hooks: make(map[HookName][]*HookHandler),
	}
}

// Register 按 order 升序插入 handler。多个同 Order 的 handler 按注册顺序排列。
func (b *ExtensionBus) Register(name HookName, order HookOrder, source string, handler func(ctx *HookContext, event map[string]any) *HookResult) error {
	if err := pluginmeta.ValidateHookName(string(name)); err != nil {
		return err
	}
	if err := pluginmeta.ValidateSource(source); err != nil {
		return err
	}
	if handler == nil {
		return fmt.Errorf("hook %q has no handler", name)
	}
	h := &HookHandler{
		Order:   order,
		Handler: handler,
		Source:  source,
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	list := b.hooks[name]
	idx := sortInsertIdx(list, order)
	newList := make([]*HookHandler, len(list)+1)
	copy(newList, list[:idx])
	newList[idx] = h
	copy(newList[idx+1:], list[idx:])
	b.hooks[name] = newList
	return nil
}

// MustRegister registers a compile-time built-in hook and panics on invalid metadata.
func (b *ExtensionBus) MustRegister(name HookName, order HookOrder, source string, handler func(ctx *HookContext, event map[string]any) *HookResult) {
	if err := b.Register(name, order, source, handler); err != nil {
		panic(err)
	}
}

// sortInsertIdx 返回 order 应插入的位置（稳定插入：相等时放到末尾以保持注册顺序）
func sortInsertIdx(list []*HookHandler, order HookOrder) int {
	for i, h := range list {
		if h.Order > order {
			return i
		}
	}
	return len(list)
}

// Emit 触发钩子并返回聚合后的最终载荷。Block、Cancel 或 context 取消会
// 停止后续 handler；单个 handler panic 会被记录并隔离。
func (b *ExtensionBus) Emit(ctx *HookContext, name HookName, event map[string]any) HookOutcome {
	b.mu.RLock()
	handlers := append([]*HookHandler(nil), b.hooks[name]...)
	b.mu.RUnlock()

	if ctx == nil {
		ctx = &HookContext{}
	}
	if ctx.Context == nil {
		ctx.Context = context.Background()
	}
	outcome := HookOutcome{Event: deepCopyMap(event)}

	for _, h := range handlers {
		if err := ctx.Context.Err(); err != nil {
			outcome.Cancelled = true
			outcome.Reason = err.Error()
			break
		}

		res, diagnostic := invokeHandler(h, ctx, deepCopyMap(outcome.Event))
		if diagnostic != nil {
			outcome.Diagnostics = append(outcome.Diagnostics, *diagnostic)
			log.New(os.Stderr, "", 0).Printf("hook panic recovered: name=%s source=%s order=%d: %s\n",
				name, h.Source, h.Order, diagnostic.Panic)
			continue
		}
		if res == nil {
			continue
		}
		if res.Transform != nil {
			outcome.Event = deepMergeMap(outcome.Event, res.Transform)
		}
		if res.Reason != "" {
			outcome.Reason = res.Reason
		}
		if res.Block {
			outcome.Blocked = true
			break
		}
		if res.Cancel {
			outcome.Cancelled = true
			break
		}
	}
	return outcome
}

func invokeHandler(h *HookHandler, ctx *HookContext, event map[string]any) (result *HookResult, diagnostic *HookDiagnostic) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			diagnostic = &HookDiagnostic{Source: h.Source, Order: h.Order, Panic: fmt.Sprint(recovered)}
		}
	}()
	return h.Handler(ctx, event), nil
}

// Clear 清空某钩子的所有 handler
func (b *ExtensionBus) Clear(name HookName) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[name] = nil
}

// ClearSource removes handlers from every hook whose Source has prefix.
func (b *ExtensionBus) ClearSource(prefix string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	removed := 0
	for name, handlers := range b.hooks {
		kept := handlers[:0]
		for _, handler := range handlers {
			if strings.HasPrefix(handler.Source, prefix) {
				removed++
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) == 0 {
			delete(b.hooks, name)
		} else {
			b.hooks[name] = kept
		}
	}
	return removed
}

// List 读取某钩子的 handler 列表（返回副本，防止外部修改）
func (b *ExtensionBus) List(name HookName) []HookHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	src := b.hooks[name]
	if src == nil {
		return nil
	}
	result := make([]HookHandler, len(src))
	for i, handler := range src {
		result[i] = *handler
	}
	return result
}

// ----- 深拷贝工具函数 -----

// deepCopyMap 递归深拷贝 map[string]any。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue 递归深拷贝任意值。
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		return deepCopySlice(val)
	default:
		return val
	}
}

// deepCopySlice 深拷贝 []any。
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = deepCopyValue(v)
	}
	return out
}

// deepMergeMap 合并两个 map（target 是基础，src 的值覆盖/新增到 target）。
// 对嵌套 map[string]any 递归合并；对其它类型直接覆盖。
func deepMergeMap(target, src map[string]any) map[string]any {
	if target == nil {
		target = make(map[string]any)
	}
	for k, v := range src {
		if tv, ok := target[k].(map[string]any); ok {
			if sv, sok := v.(map[string]any); sok {
				target[k] = deepMergeMap(deepCopyMap(tv), sv)
			} else {
				target[k] = deepCopyValue(v)
			}
		} else {
			target[k] = deepCopyValue(v)
		}
	}
	return target
}
