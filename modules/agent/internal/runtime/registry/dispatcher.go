package registry

import (
	"context"
	"sync"

	jsonrpc "redpanda/protocol/jsonrpc"
)

// MethodHandler JSON-RPC 方法处理器。
type MethodHandler func(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error)

// Dispatcher RPC 分发器。
type Dispatcher struct {
	mu      sync.RWMutex
	methods map[string]MethodHandler
}

// NewDispatcher 创建空分发器。
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		methods: make(map[string]MethodHandler),
	}
}

// Register 注册一个 JSON-RPC 方法处理器。
func (d *Dispatcher) Register(method string, h MethodHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.methods[method] = h
}

// Dispatch 根据 req.Method 分发请求。未找到处理器时返回 method-not-found 错误。
func (d *Dispatcher) Dispatch(ctx context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
	d.mu.RLock()
	handler, ok := d.methods[req.Method]
	d.mu.RUnlock()

	if !ok {
		return jsonrpc.NewError(req.ID, -32601, "method not found"), nil
	}

	return handler(ctx, req)
}
