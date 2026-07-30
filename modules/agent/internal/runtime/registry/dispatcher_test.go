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
