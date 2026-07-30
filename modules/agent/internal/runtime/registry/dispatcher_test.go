package registry

import (
	"context"
	"sync"
	"testing"

	jsonrpc "redpanda/protocol/jsonrpc"
)

func TestDispatcherRegisterAndDispatch(t *testing.T) {
	d := NewDispatcher()
	d.Register("test.echo", func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "echoed")
	})

	req := jsonrpc.Request{JSONRPC: "2.0", ID: "1", Method: "test.echo"}
	resp, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("Dispatch error: %v", err)
	}
	if resp.ID != "1" {
		t.Fatalf("response id = %q", resp.ID)
	}
}

func TestDispatcherMethodNotFound(t *testing.T) {
	d := NewDispatcher()
	req := jsonrpc.Request{JSONRPC: "2.0", ID: "1", Method: "unknown"}
	resp, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", resp.Error.Code)
	}
	if resp.Error.Message != "method not found" {
		t.Fatalf("error message = %q", resp.Error.Message)
	}
}

func TestDispatcherOverrideAndRestore(t *testing.T) {
	d := NewDispatcher()
	d.RegisterFrom("builtin:runtime", "test.echo", func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "builtin")
	})
	d.RegisterFrom("js:plugin", "test.echo", func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "plugin")
	})

	list := d.List()
	if len(list) != 1 || list[0].Source != "js:plugin" || list[0].Overridden != "builtin:runtime" {
		t.Fatalf("unexpected list: %#v", list)
	}
	if removed := d.ClearSource("js:"); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	list = d.List()
	if len(list) != 1 || list[0].Source != "builtin:runtime" || list[0].Overridden != "" {
		t.Fatalf("unexpected restored list: %#v", list)
	}
	resp, err := d.Dispatch(context.Background(), jsonrpc.Request{ID: "1", Method: "test.echo"})
	if err != nil || resp.Error != nil {
		t.Fatalf("restored dispatch failed: %#v, %v", resp, err)
	}
}

func TestDispatcherUnregister(t *testing.T) {
	d := NewDispatcher()
	d.RegisterFrom("js:plugin", "test.method", func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "ok")
	})
	if !d.Unregister("test.method", "js:plugin") {
		t.Fatal("expected unregister")
	}
	if d.Unregister("test.method", "js:plugin") {
		t.Fatal("second unregister should report false")
	}
	if list := d.List(); len(list) != 0 {
		t.Fatalf("list after unregister: %#v", list)
	}
}

func TestDispatcherClearSourceRemovesEveryMethod(t *testing.T) {
	d := NewDispatcher()
	for _, method := range []string{"plugin.one", "plugin.two", "plugin.three"} {
		d.RegisterFrom("js:plugin", method, func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
			return jsonrpc.NewResult(req.ID, "ok")
		})
	}
	d.RegisterFrom("builtin:runtime", "core.ping", func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "pong")
	})

	if removed := d.ClearSource("js:"); removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	list := d.List()
	if len(list) != 1 || list[0].Method != "core.ping" {
		t.Fatalf("list after clear = %#v", list)
	}
}

func TestDispatcherRejectsInvalidRegistration(t *testing.T) {
	d := NewDispatcher()
	handler := func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
		return jsonrpc.NewResult(req.ID, "ok")
	}
	if _, err := d.RegisterFrom("js:plugin", "bad method", handler); err == nil {
		t.Fatal("expected invalid method error")
	}
	if _, err := d.RegisterFrom("plugin:legacy", "valid.method", handler); err == nil {
		t.Fatal("expected invalid source error")
	}
	if _, err := d.RegisterFrom("js:plugin", "valid.method", nil); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestDispatcherConcurrentRegister(t *testing.T) {
	d := NewDispatcher()
	var wg sync.WaitGroup
	n := 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			method := "test.method"
			d.Register(method, func(_ context.Context, req jsonrpc.Request) (jsonrpc.Response, error) {
				return jsonrpc.NewResult(req.ID, "ok")
			})
		}(i)
	}
	wg.Wait()

	req := jsonrpc.Request{JSONRPC: "2.0", ID: "1", Method: "test.method"}
	resp, err := d.Dispatch(context.Background(), req)
	if err != nil {
		t.Fatalf("Dispatch after concurrent register: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected result, got error: %v", resp.Error)
	}
}
