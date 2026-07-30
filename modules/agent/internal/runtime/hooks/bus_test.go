package hooks

import (
	"sync"
	"testing"
)

// TestBusRegisterAndEmit 注册 2 个 handler → Emit 按 order 执行 → Transform 正确链式传递
func TestBusRegisterAndEmit(t *testing.T) {
	bus := NewBus()
	name := HookName("test.chain")
	var callOrder []string

	bus.Register(name, 10, "a", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "a")
		return &HookResult{
			Transform: map[string]any{"step": "a", "value": "1"},
		}
	})
	bus.Register(name, 20, "b", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "b")
		// 应该看到 a 注入的 step=a
		if event["step"] != "a" {
			t.Errorf("expected event.step==\"a\", got %v", event["step"])
		}
		return &HookResult{
			Transform: map[string]any{"step": "b", "value": "2"},
		}
	})

	ctx := &HookContext{}
	result := bus.Emit(ctx, name, map[string]any{"base": "x"})

	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.Transform == nil {
		t.Fatalf("expected non-nil Transform")
	}
	// result 是最后一个 handler 的返回值，其 Transform 只包含该 handler 的 patch
	if result.Transform["step"] != "b" {
		t.Errorf("expected step==\"b\", got %v", result.Transform["step"])
	}
	if result.Transform["value"] != "2" {
		t.Errorf("expected value==\"2\", got %v", result.Transform["value"])
	}
	// "base" 在链式合并的 payload 中，但不在最后一个 handler 的 Transform 中
	if result.Transform["base"] != nil {
		t.Errorf("expected base==nil in last Transform, got %v", result.Transform["base"])
	}
	if len(callOrder) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(callOrder))
	}
	if callOrder[0] != "a" || callOrder[1] != "b" {
		t.Errorf("expected call order [a b], got %v", callOrder)
	}
}

// TestBusBlockShortCircuit 第一个 handler 返回 {Block: true, Reason: "blocked"} → 第二个 handler 未被调用
func TestBusBlockShortCircuit(t *testing.T) {
	bus := NewBus()
	name := HookName("test.block")
	calledB := false

	bus.Register(name, 10, "a", func(ctx *HookContext, event map[string]any) *HookResult {
		return &HookResult{Block: true, Reason: "blocked"}
	})
	bus.Register(name, 20, "b", func(ctx *HookContext, event map[string]any) *HookResult {
		calledB = true
		return nil
	})

	result := bus.Emit(&HookContext{}, name, map[string]any{})
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if !result.Block {
		t.Fatalf("expected Block==true")
	}
	if result.Reason != "blocked" {
		t.Errorf("expected Reason==\"blocked\", got %q", result.Reason)
	}
	if calledB {
		t.Fatalf("second handler should not have been called")
	}
}

// TestBusPanicRecover 中间 handler panic → 后续 handler 仍执行 → Emit 不 panic
func TestBusPanicRecover(t *testing.T) {
	bus := NewBus()
	name := HookName("test.panic")
	var callOrder []string

	bus.Register(name, 10, "a", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "a")
		return nil
	})
	bus.Register(name, 20, "b", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "b")
		panic("boom")
	})
	bus.Register(name, 30, "c", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "c")
		return nil
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Emit should not panic, got: %v", r)
		}
	}()

	bus.Emit(&HookContext{}, name, map[string]any{})

	if len(callOrder) != 3 {
		t.Fatalf("expected 3 calls (including panicked one), got %d: %v", len(callOrder), callOrder)
	}
	expected := []string{"a", "b", "c"}
	if callOrder[0] != expected[0] || callOrder[1] != expected[1] || callOrder[2] != expected[2] {
		t.Errorf("expected call order %v, got %v", expected, callOrder)
	}
}

// TestBusOrder Order=0 先执行，Order=100 后执行
func TestBusOrder(t *testing.T) {
	bus := NewBus()
	name := HookName("test.order")
	var callOrder []string

	bus.Register(name, HookOrderPlugin, "plugin", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "plugin")
		return nil
	})
	bus.Register(name, HookOrderSystem, "system", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "system")
		return nil
	})

	bus.Emit(&HookContext{}, name, map[string]any{})

	if len(callOrder) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(callOrder))
	}
	if callOrder[0] != "system" || callOrder[1] != "plugin" {
		t.Errorf("expected [system plugin], got %v", callOrder)
	}
}

// TestBusClear Clear 后该钩子返回 nil
func TestBusClear(t *testing.T) {
	bus := NewBus()
	name := HookName("test.clear")

	bus.Register(name, 10, "a", func(ctx *HookContext, event map[string]any) *HookResult {
		return &HookResult{Transform: map[string]any{"k": "v"}}
	})

	result := bus.Emit(&HookContext{}, name, map[string]any{})
	if result == nil {
		t.Fatalf("expected result before clear")
	}

	bus.Clear(name)

	result = bus.Emit(&HookContext{}, name, map[string]any{})
	if result != nil {
		t.Fatalf("expected nil result after clear, got %v", result)
	}

	if list := bus.List(name); list != nil {
		t.Fatalf("expected nil list after clear, got %d items", len(list))
	}
}

// TestBusConcurrentRegisterEmit 并发 Register + Emit 不 panic
func TestBusConcurrentRegisterEmit(t *testing.T) {
	bus := NewBus()
	name := HookName("test.concurrent")
	var wg sync.WaitGroup
	const n = 100

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			order := HookOrder(idx % 5 * 10)
			bus.Register(name, order, "goroutine", func(ctx *HookContext, event map[string]any) *HookResult {
				if event["key"] == "trigger" {
					return &HookResult{Transform: map[string]any{"count": 1}}
				}
				return nil
			})
		}(i)
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = bus.Emit(&HookContext{}, name, map[string]any{"key": "trigger"})
		}()
	}

	wg.Wait()
}
