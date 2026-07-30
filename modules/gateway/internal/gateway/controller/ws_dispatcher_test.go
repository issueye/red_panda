package controller

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	protows "redpanda/protocol/ws"
)

func TestWSDispatcherOverrideAndRestore(t *testing.T) {
	dispatcher := NewWSDispatcher()
	var calls []string
	builtin := func(context.Context, *WSRequestContext, protows.Envelope) { calls = append(calls, "builtin") }
	plugin := func(context.Context, *WSRequestContext, protows.Envelope) { calls = append(calls, "plugin") }
	dispatcher.Register("test.method", builtin)
	previous, err := dispatcher.RegisterFrom("js:test", "test.method", plugin)
	if err != nil {
		t.Fatal(err)
	}
	if previous == nil || previous.Source != "builtin:gateway" {
		t.Fatalf("previous = %#v", previous)
	}
	if !dispatcher.Dispatch(context.Background(), &WSRequestContext{}, protows.Envelope{Method: "test.method"}) {
		t.Fatal("registered method was not dispatched")
	}
	if !reflect.DeepEqual(calls, []string{"plugin"}) {
		t.Fatalf("calls = %#v", calls)
	}
	if !dispatcher.Unregister("test.method", "js:test") {
		t.Fatal("plugin layer was not removed")
	}
	dispatcher.Dispatch(context.Background(), &WSRequestContext{}, protows.Envelope{Method: "test.method"})
	if !reflect.DeepEqual(calls, []string{"plugin", "builtin"}) {
		t.Fatalf("calls after restore = %#v", calls)
	}
}

func TestWSDispatcherClearSourceAndList(t *testing.T) {
	dispatcher := NewWSDispatcher()
	handler := func(context.Context, *WSRequestContext, protows.Envelope) {}
	dispatcher.Register("test.first", handler)
	dispatcher.Register("test.second", handler)
	if _, err := dispatcher.RegisterFrom("js:test", "test.first", handler); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.RegisterFrom("js:test", "test.third", handler); err != nil {
		t.Fatal(err)
	}
	if removed := dispatcher.ClearSource("js:"); removed != 2 {
		t.Fatalf("removed = %d", removed)
	}
	got := dispatcher.List()
	want := []WSMethodInfo{
		{Method: "test.first", Source: "builtin:gateway"},
		{Method: "test.second", Source: "builtin:gateway"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %#v, want %#v", got, want)
	}
	if dispatcher.Dispatch(context.Background(), &WSRequestContext{}, protows.Envelope{Method: "test.third"}) {
		t.Fatal("cleared method still dispatched")
	}
}

func TestGatewayWSDispatcherBuiltins(t *testing.T) {
	infos := newGatewayWSDispatcher().List()
	got := make([]string, 0, len(infos))
	for _, info := range infos {
		if info.Source != "builtin:gateway" {
			t.Fatalf("builtin source = %q", info.Source)
		}
		got = append(got, info.Method)
	}
	want := []string{
		protows.MethodAgentStatus,
		protows.MethodRunStart,
		protows.MethodRunSubscribe,
		protows.MethodRunResume,
		protows.MethodRunCancel,
		protows.MethodWorkerList,
		protows.MethodAssignmentCancel,
		protows.MethodPermissionResolve,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("methods = %#v, want %#v", got, want)
	}
}

func TestWSDispatcherRejectsInvalidMetadata(t *testing.T) {
	dispatcher := NewWSDispatcher()
	handler := func(context.Context, *WSRequestContext, protows.Envelope) {}
	if _, err := dispatcher.RegisterFrom("plugin:test", "test.method", handler); err == nil {
		t.Fatal("expected invalid source error")
	}
	if _, err := dispatcher.RegisterFrom("js:test", "has space", handler); err == nil {
		t.Fatal("expected invalid method error")
	}
	if _, err := dispatcher.RegisterFrom("js:test", "test.method", nil); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestWSDispatcherConcurrentAccess(t *testing.T) {
	dispatcher := NewWSDispatcher()
	dispatcher.Register("test.method", func(context.Context, *WSRequestContext, protows.Envelope) {})
	var wg sync.WaitGroup
	for index := 0; index < 100; index++ {
		wg.Add(3)
		go func(index int) {
			defer wg.Done()
			_, _ = dispatcher.RegisterFrom(fmt.Sprintf("js:worker-%d", index), "test.method", func(context.Context, *WSRequestContext, protows.Envelope) {})
		}(index)
		go func() {
			defer wg.Done()
			dispatcher.Dispatch(context.Background(), &WSRequestContext{}, protows.Envelope{Method: "test.method"})
		}()
		go func() {
			defer wg.Done()
			dispatcher.ClearSource("js:")
		}()
	}
	wg.Wait()
	dispatcher.ClearSource("js:")
	infos := dispatcher.List()
	if len(infos) != 1 || infos[0].Source != "builtin:gateway" {
		t.Fatalf("builtin not restored: %#v", infos)
	}
}
