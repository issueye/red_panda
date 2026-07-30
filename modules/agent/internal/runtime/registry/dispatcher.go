package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"

	jsonrpc "redpanda/protocol/jsonrpc"
	"redpanda/protocol/pluginmeta"
)

// MethodHandler JSON-RPC 方法处理器。
type MethodHandler func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error)

type MethodEntry struct {
	Method     string
	Source     string
	Overridden string
	Handler    MethodHandler
}

type MethodInfo struct {
	Method     string
	Source     string
	Overridden string
}

// Dispatcher RPC 分发器。
type Dispatcher struct {
	mu      sync.RWMutex
	methods map[string][]MethodEntry
	order   []string
}

// NewDispatcher 创建空分发器。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		methods: make(map[string][]MethodEntry),
	}
}

// Register registers a compile-time built-in Runtime method.
func (d *Dispatcher) Register(method string, h MethodHandler) {
	if _, err := d.RegisterFrom("builtin:runtime", method, h); err != nil {
		panic(err)
	}
}

// RegisterFrom registers a method from source. Later registrations override
// earlier sources; re-registering the same source replaces its prior layer.
func (d *Dispatcher) RegisterFrom(source, method string, h MethodHandler) (*MethodEntry, error) {
	if err := pluginmeta.ValidateMethod(method); err != nil {
		return nil, err
	}
	if err := pluginmeta.ValidateSource(source); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, fmt.Errorf("method %q has no handler", method)
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	stack := d.methods[method]
	var previous *MethodEntry
	if len(stack) > 0 {
		entry := stack[len(stack)-1]
		entry.Overridden = ""
		previous = &entry
	}
	kept := make([]MethodEntry, 0, len(stack))
	for _, entry := range stack {
		if entry.Source != source {
			kept = append(kept, entry)
		}
	}
	entry := MethodEntry{Method: method, Source: source, Handler: h}
	if len(kept) > 0 {
		entry.Overridden = kept[len(kept)-1].Source
	}
	d.methods[method] = append(kept, entry)
	if !containsMethod(d.order, method) {
		d.order = append(d.order, method)
	}
	return previous, nil
}

// Dispatch 根据 req.Method 分发请求。未找到处理器时返回 method-not-found 错误。
func (d *Dispatcher) Dispatch(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
	d.mu.RLock()
	stack := d.methods[req.Method]
	var handler MethodHandler
	if len(stack) > 0 {
		handler = stack[len(stack)-1].Handler
	}
	d.mu.RUnlock()

	if handler == nil {
		return jsonrpc.NewError(req.ID, -32601, "method not found"), nil
	}

	return handler(ctx, req)
}

// Unregister removes one source layer and restores the previous handler.
func (d *Dispatcher) Unregister(method, source string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	stack := d.methods[method]
	kept, removed := removeMethodSource(stack, source)
	if !removed {
		return false
	}
	d.storeStack(method, kept)
	return true
}

// ClearSource removes all method layers whose source has prefix.
func (d *Dispatcher) ClearSource(prefix string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	removed := 0
	for _, method := range append([]string(nil), d.order...) {
		stack := d.methods[method]
		kept := make([]MethodEntry, 0, len(stack))
		for _, entry := range stack {
			if strings.HasPrefix(entry.Source, prefix) {
				removed++
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(d.methods, method)
		} else {
			d.methods[method] = normalizeMethodOverrides(kept)
		}
	}
	d.compactOrder()
	return removed
}

// List returns active methods in first-registration order.
func (d *Dispatcher) List() []MethodInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]MethodInfo, 0, len(d.order))
	for _, method := range d.order {
		stack := d.methods[method]
		if len(stack) == 0 {
			continue
		}
		entry := stack[len(stack)-1]
		result = append(result, MethodInfo{Method: method, Source: entry.Source, Overridden: entry.Overridden})
	}
	return result
}

func (d *Dispatcher) storeStack(method string, stack []MethodEntry) {
	if len(stack) == 0 {
		delete(d.methods, method)
		d.compactOrder()
		return
	}
	d.methods[method] = normalizeMethodOverrides(stack)
}

func normalizeMethodOverrides(stack []MethodEntry) []MethodEntry {
	for i := range stack {
		stack[i].Overridden = ""
		if i > 0 {
			stack[i].Overridden = stack[i-1].Source
		}
	}
	return stack
}

func (d *Dispatcher) compactOrder() {
	order := d.order[:0]
	for _, method := range d.order {
		if len(d.methods[method]) > 0 {
			order = append(order, method)
		}
	}
	d.order = order
}

func removeMethodSource(stack []MethodEntry, source string) ([]MethodEntry, bool) {
	kept := make([]MethodEntry, 0, len(stack))
	removed := false
	for _, entry := range stack {
		if entry.Source == source {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	return kept, removed
}

func containsMethod(methods []string, target string) bool {
	for _, method := range methods {
		if method == target {
			return true
		}
	}
	return false
}
