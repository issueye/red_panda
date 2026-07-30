package hooks

import (
	"context"
	"sync"
	"testing"
)

// TestBusRegisterAndEmit 注册 2 个 handler → Emit 按 order 执行 → Transform 正确链式传递
func TestBusRegisterAndEmit(t *testing.T) {
	bus := NewBus()
	name := HookName("test.chain")
	var callOrder []string

	bus.Register(name, 10, "js:a", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "a")
		return &HookResult{
			Transform: map[string]any{"step": "a", "value": "1"},
		}
	})
	bus.Register(name, 20, "js:b", func(ctx *HookContext, event map[string]any) *HookResult {
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

	if result.Event["step"] != "b" {
		t.Errorf("expected step==\"b\", got %v", result.Event["step"])
	}
	if result.Event["value"] != "2" {
		t.Errorf("expected value==\"2\", got %v", result.Event["value"])
	}
	if result.Event["base"] != "x" {
		t.Errorf("expected base==x in final event, got %v", result.Event["base"])
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

	bus.Register(name, 10, "js:a", func(ctx *HookContext, event map[string]any) *HookResult {
		return &HookResult{Block: true, Reason: "blocked"}
	})
	bus.Register(name, 20, "js:b", func(ctx *HookContext, event map[string]any) *HookResult {
		calledB = true
		return nil
	})

	result := bus.Emit(&HookContext{}, name, map[string]any{})
	if !result.Blocked {
		t.Fatalf("expected Blocked==true")
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

	bus.Register(name, 10, "js:a", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "a")
		return nil
	})
	bus.Register(name, 20, "js:b", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "b")
		panic("boom")
	})
	bus.Register(name, 30, "js:c", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "c")
		return nil
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Emit should not panic, got: %v", r)
		}
	}()

	result := bus.Emit(&HookContext{}, name, map[string]any{})

	if len(callOrder) != 3 {
		t.Fatalf("expected 3 calls (including panicked one), got %d: %v", len(callOrder), callOrder)
	}
	expected := []string{"a", "b", "c"}
	if callOrder[0] != expected[0] || callOrder[1] != expected[1] || callOrder[2] != expected[2] {
		t.Errorf("expected call order %v, got %v", expected, callOrder)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Source != "js:b" || result.Diagnostics[0].Panic != "boom" {
		t.Fatalf("unexpected diagnostics: %#v", result.Diagnostics)
	}
}

// TestBusOrder Order=0 先执行，Order=100 后执行
func TestBusOrder(t *testing.T) {
	bus := NewBus()
	name := HookName("test.order")
	var callOrder []string

	bus.Register(name, HookOrderPlugin, "js:plugin", func(ctx *HookContext, event map[string]any) *HookResult {
		callOrder = append(callOrder, "plugin")
		return nil
	})
	bus.Register(name, HookOrderSystem, "builtin:system", func(ctx *HookContext, event map[string]any) *HookResult {
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

	bus.Register(name, 10, "js:a", func(ctx *HookContext, event map[string]any) *HookResult {
		return &HookResult{Transform: map[string]any{"k": "v"}}
	})

	result := bus.Emit(&HookContext{}, name, map[string]any{})
	if result.Event["k"] != "v" {
		t.Fatalf("expected transform before clear: %#v", result.Event)
	}

	bus.Clear(name)

	result = bus.Emit(&HookContext{}, name, map[string]any{})
	if len(result.Event) != 0 {
		t.Fatalf("expected unchanged empty event after clear, got %v", result.Event)
	}

	if list := bus.List(name); list != nil {
		t.Fatalf("expected nil list after clear, got %d items", len(list))
	}
}

func TestBusCancelAndContextCancellation(t *testing.T) {
	bus := NewBus()
	name := HookName("test.cancel")
	called := false
	bus.Register(name, 10, "js:one", func(_ *HookContext, _ map[string]any) *HookResult {
		return &HookResult{Cancel: true, Reason: "cancelled by plugin"}
	})
	bus.Register(name, 20, "js:two", func(_ *HookContext, _ map[string]any) *HookResult {
		called = true
		return nil
	})

	outcome := bus.Emit(&HookContext{}, name, nil)
	if !outcome.Cancelled || outcome.Reason != "cancelled by plugin" || called {
		t.Fatalf("unexpected cancel outcome: %#v called=%v", outcome, called)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	outcome = bus.Emit(&HookContext{Context: cancelled}, name, nil)
	if !outcome.Cancelled || outcome.Reason != context.Canceled.Error() {
		t.Fatalf("unexpected context outcome: %#v", outcome)
	}
}

func TestBusClearSource(t *testing.T) {
	bus := NewBus()
	first := HookName("test.first")
	second := HookName("test.second")
	bus.Register(first, 10, "builtin:core", func(_ *HookContext, _ map[string]any) *HookResult { return nil })
	bus.Register(first, 20, "js:plugin", func(_ *HookContext, _ map[string]any) *HookResult { return nil })
	bus.Register(second, 10, "js:plugin", func(_ *HookContext, _ map[string]any) *HookResult { return nil })

	if removed := bus.ClearSource("js:plugin"); removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	if got := bus.List(first); len(got) != 1 || got[0].Source != "builtin:core" {
		t.Fatalf("first handlers = %#v", got)
	}
	if got := bus.List(second); got != nil {
		t.Fatalf("second handlers = %#v", got)
	}
}

func TestBusRejectsInvalidRegistration(t *testing.T) {
	bus := NewBus()
	handler := func(_ *HookContext, _ map[string]any) *HookResult { return nil }
	if err := bus.Register("bad hook", 10, "js:plugin", handler); err == nil {
		t.Fatal("expected invalid hook name error")
	}
	if err := bus.Register("valid.hook", 10, "plugin:legacy", handler); err == nil {
		t.Fatal("expected invalid source error")
	}
	if err := bus.Register("valid.hook", 10, "js:plugin", nil); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestBusHandlerCannotMutateOutcomeWithoutTransform(t *testing.T) {
	bus := NewBus()
	name := HookName("test.immutable")
	bus.MustRegister(name, 10, "builtin:test", func(_ *HookContext, event map[string]any) *HookResult {
		event["value"] = "mutated"
		return nil
	})

	outcome := bus.Emit(&HookContext{}, name, map[string]any{"value": "original"})
	if outcome.Event["value"] != "original" {
		t.Fatalf("handler mutation leaked into outcome: %#v", outcome.Event)
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
			bus.Register(name, order, "js:goroutine", func(ctx *HookContext, event map[string]any) *HookResult {
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
