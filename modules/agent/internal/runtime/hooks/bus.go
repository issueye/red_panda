package hooks

import (
	"fmt"
	"log"
	"os"
	"sync"
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

// HookHandler 钩子处理器
type HookHandler struct {
	Order   HookOrder
	Handler func(ctx *HookContext, event map[string]any) *HookResult
	Source  string // "builtin" | "plugin:xxx"
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
func (b *ExtensionBus) Register(name HookName, order HookOrder, source string, handler func(ctx *HookContext, event map[string]any) *HookResult) {
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

// Emit 触发钩子，链式执行 handler。
// 复制 event 作为当前载荷，依次调用 handler，合并 Transform。
// 任一 handler 返回 {Block: true} 则短路后续链。
// 某 handler panic → defer recover → 记录日志 → 继续下一 handler。
// 无 handler → 返回 nil。
func (b *ExtensionBus) Emit(ctx *HookContext, name HookName, event map[string]any) *HookResult {
	b.mu.RLock()
	handlers := b.hooks[name]
	b.mu.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	payLoad := deepCopyMap(event)
	var lastResult *HookResult

	for _, h := range handlers {
		// 用独立函数 + defer recover 包裹每个 handler 调用
		call := func() *HookResult {
			return h.Handler(ctx, payLoad)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.New(os.Stderr, "", 0).Print(fmt.Sprintf("hook panic recovered: name=%s source=%s order=%d: %v\n",
						name, h.Source, h.Order, r))
				}
			}()
			res := call()
			if res == nil {
				return
			}
			lastResult = res
			if res.Block {
				return
			}
			if res.Transform != nil {
				payLoad = deepMergeMap(deepCopyMap(payLoad), res.Transform)
			}
		}()
		if lastResult != nil && lastResult.Block {
			return lastResult
		}
	}
	return lastResult
}

// Clear 清空某钩子的所有 handler
func (b *ExtensionBus) Clear(name HookName) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hooks[name] = nil
}

// List 读取某钩子的 handler 列表（返回副本，防止外部修改）
func (b *ExtensionBus) List(name HookName) []*HookHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	src := b.hooks[name]
	if src == nil {
		return nil
	}
	result := make([]*HookHandler, len(src))
	copy(result, src)
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

// Ensure fmt is used.
var _ = fmt.Sprintf
