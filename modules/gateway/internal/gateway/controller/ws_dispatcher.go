package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"redpanda/gateway/internal/gateway/service"
	"redpanda/protocol/pluginmeta"
	protows "redpanda/protocol/ws"
)

type WSRequestHandler func(context.Context, *WSRequestContext, protows.Envelope)

type WSRequestContext struct {
	session *wsSession
}

func (c *WSRequestContext) Services() service.Set { return c.session.services }

func (c *WSRequestContext) Respond(id string, payload any) {
	c.session.enqueue(response(id, payload))
}

func (c *WSRequestContext) Fail(id, code, message string) {
	c.session.enqueue(errorMessage(id, code, message))
}

func (c *WSRequestContext) Subscribe(runID string) { c.session.subscribe(runID) }

func (c *WSRequestContext) Replay(runID string, afterSeq uint64) {
	c.session.replay(runID, afterSeq)
}

type WSMethodEntry struct {
	Method     string
	Source     string
	Overridden string
	Handler    WSRequestHandler
}

type WSMethodInfo struct {
	Method     string
	Source     string
	Overridden string
}

type WSDispatcher struct {
	mu      sync.RWMutex
	methods map[string][]WSMethodEntry
	order   []string
}

func NewWSDispatcher() *WSDispatcher {
	return &WSDispatcher{methods: make(map[string][]WSMethodEntry)}
}

func (d *WSDispatcher) Register(method string, handler WSRequestHandler) {
	if _, err := d.RegisterFrom("builtin:gateway", method, handler); err != nil {
		panic(err)
	}
}

func (d *WSDispatcher) RegisterFrom(source, method string, handler WSRequestHandler) (*WSMethodEntry, error) {
	if err := pluginmeta.ValidateMethod(method); err != nil {
		return nil, err
	}
	if err := pluginmeta.ValidateSource(source); err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, fmt.Errorf("websocket method %q has no handler", method)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	stack := d.methods[method]
	var previous *WSMethodEntry
	if len(stack) > 0 {
		entry := stack[len(stack)-1]
		entry.Overridden = ""
		previous = &entry
	}
	kept := make([]WSMethodEntry, 0, len(stack))
	for _, entry := range stack {
		if entry.Source != source {
			kept = append(kept, entry)
		}
	}
	entry := WSMethodEntry{Method: method, Source: source, Handler: handler}
	if len(kept) > 0 {
		entry.Overridden = kept[len(kept)-1].Source
	}
	d.methods[method] = append(kept, entry)
	if !containsWSMethod(d.order, method) {
		d.order = append(d.order, method)
	}
	return previous, nil
}

func (d *WSDispatcher) Dispatch(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) bool {
	d.mu.RLock()
	stack := d.methods[message.Method]
	var handler WSRequestHandler
	if len(stack) > 0 {
		handler = stack[len(stack)-1].Handler
	}
	d.mu.RUnlock()
	if handler == nil {
		return false
	}
	handler(ctx, requestCtx, message)
	return true
}

func (d *WSDispatcher) Unregister(method, source string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	stack := d.methods[method]
	kept := make([]WSMethodEntry, 0, len(stack))
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
	d.storeStack(method, kept)
	return true
}

func (d *WSDispatcher) ClearSource(prefix string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	removed := 0
	for _, method := range append([]string(nil), d.order...) {
		kept := make([]WSMethodEntry, 0, len(d.methods[method]))
		for _, entry := range d.methods[method] {
			if strings.HasPrefix(entry.Source, prefix) {
				removed++
				continue
			}
			kept = append(kept, entry)
		}
		d.storeStack(method, kept)
	}
	return removed
}

func (d *WSDispatcher) List() []WSMethodInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]WSMethodInfo, 0, len(d.order))
	for _, method := range d.order {
		stack := d.methods[method]
		if len(stack) == 0 {
			continue
		}
		entry := stack[len(stack)-1]
		result = append(result, WSMethodInfo{Method: method, Source: entry.Source, Overridden: entry.Overridden})
	}
	return result
}

func (d *WSDispatcher) storeStack(method string, stack []WSMethodEntry) {
	if len(stack) == 0 {
		delete(d.methods, method)
		d.compactOrder()
		return
	}
	for index := range stack {
		stack[index].Overridden = ""
		if index > 0 {
			stack[index].Overridden = stack[index-1].Source
		}
	}
	d.methods[method] = stack
}

func (d *WSDispatcher) compactOrder() {
	order := d.order[:0]
	for _, method := range d.order {
		if len(d.methods[method]) > 0 {
			order = append(order, method)
		}
	}
	d.order = order
}

func containsWSMethod(methods []string, target string) bool {
	for _, method := range methods {
		if method == target {
			return true
		}
	}
	return false
}

func newGatewayWSDispatcher() *WSDispatcher {
	dispatcher := NewWSDispatcher()
	dispatcher.Register(protows.MethodAgentStatus, func(_ context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.Respond(message.ID, requestCtx.Services().Run.RuntimeStatus())
	})
	dispatcher.Register(protows.MethodRunStart, func(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleRunStart(ctx, message)
	})
	dispatcher.Register(protows.MethodRunSubscribe, func(_ context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleRunSubscribe(message)
	})
	dispatcher.Register(protows.MethodRunResume, func(_ context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleRunResume(message)
	})
	dispatcher.Register(protows.MethodRunCancel, func(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleRunCancel(ctx, message)
	})
	dispatcher.Register(protows.MethodWorkerList, func(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleWorkerList(ctx, message)
	})
	dispatcher.Register(protows.MethodAssignmentCancel, func(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handleAssignmentCancel(ctx, message)
	})
	dispatcher.Register(protows.MethodPermissionResolve, func(ctx context.Context, requestCtx *WSRequestContext, message protows.Envelope) {
		requestCtx.session.handlePermissionResolve(ctx, message)
	})
	return dispatcher
}
