package runtimeclient

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"redpanda/protocol/jsonrpc"
	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
)

func TestClientHandlesRuntimeOriginatedRequest(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != methods.MemoryToolExecute {
			t.Fatalf("method = %s, want %s", method, methods.MemoryToolExecute)
		}
		var req methods.MemoryToolExecuteParams
		if err := json.Unmarshal(params, &req); err != nil {
			t.Fatal(err)
		}
		if req.ToolName != "memory.list" {
			t.Fatalf("tool name = %s, want memory.list", req.ToolName)
		}
		return methods.MemoryToolExecuteResult{Status: "completed", Output: "[]"}, nil
	})
	client.running = true
	client.stdin = writer

	request, err := jsonrpc.NewRequest("rt_1", methods.MemoryToolExecute, methods.MemoryToolExecuteParams{
		RunID:      "run_1",
		ToolCallID: "tool_1",
		ToolName:   "memory.list",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		client.handleRequest(raw)
		close(done)
	}()

	var resp jsonrpc.Response
	if err := json.NewDecoder(reader).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	<-done
	if resp.ID != "rt_1" || resp.Error != nil {
		t.Fatalf("unexpected response: %#v", resp)
	}
	var result methods.MemoryToolExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Output != "[]" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestClientDiscoversMCPThroughRuntime(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, nil)
	client.running = true
	client.stdin = writer

	done := make(chan struct{})
	go func() {
		defer close(done)
		decoder := json.NewDecoder(reader)
		for index := 0; index < 2; index++ {
			var req jsonrpc.Request
			if err := decoder.Decode(&req); err != nil {
				return
			}
			var result any = methods.InitializeResult{}
			if req.Method == methods.MCPDiscover {
				result = protocolmcp.MCPDiscoveryResult{Servers: []protocolmcp.MCPServerDiscovery{{
					Name: "filesystem", Status: "ready", Tools: []protocolmcp.MCPToolDefinition{{Name: "read_file", InputSchema: map[string]any{"type": "object"}}},
				}}}
			}
			resp, _ := jsonrpc.NewResult(req.ID, result)
			raw, _ := json.Marshal(resp)
			_ = client.handleGatewayLikeResponseForTest(raw)
		}
	}()

	result, err := client.DiscoverMCP(context.Background(), methods.MCPDiscoverParams{
		Servers: []protocolmcp.MCPServerConfig{{Name: "filesystem", Command: "mcp-filesystem", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if len(result.Servers) != 1 || result.Servers[0].Status != "ready" || len(result.Servers[0].Tools) != 1 {
		t.Fatalf("unexpected discovery result: %#v", result)
	}
}

func (c *Client) handleGatewayLikeResponseForTest(raw []byte) error {
	var resp jsonrpc.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	c.mu.Lock()
	ch := c.pending[resp.ID]
	delete(c.pending, resp.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- resp
	}
	return nil
}
