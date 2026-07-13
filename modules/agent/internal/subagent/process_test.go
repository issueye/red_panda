package subagent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

// Unit test for gateway request proxy without depending on the runtime package.
func TestSubAgentProcessProxiesGatewayRequestAndReturnsResponse(t *testing.T) {
	childReader, childWriter := io.Pipe()
	defer childReader.Close()
	defer childWriter.Close()

	// Mock gateway: echo a completed memory.create result for any MemoryToolExecute.
	onRequest := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if method != methods.MemoryToolExecute {
			t.Fatalf("unexpected method %s", method)
		}
		raw, err := json.Marshal(methods.MemoryToolExecuteResult{
			Status:   "completed",
			Output:   `{"action":"memory.create"}`,
			RecordID: "mem_child_1",
		})
		return raw, err
	}

	child := &subAgentProcess{
		stdin:     childWriter,
		running:   true,
		pending:   map[jsonrpc.ID]chan jsonrpc.Response{},
		onRequest: onRequest,
	}

	childRequest, err := jsonrpc.NewRequest("rt_child_1", methods.MemoryToolExecute, methods.MemoryToolExecuteParams{
		RunID:      "run_child_memory",
		SessionID:  "session_child_memory",
		ToolCallID: "tool_child_memory",
		ToolName:   "memory.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	requestLine, err := json.Marshal(childRequest)
	if err != nil {
		t.Fatal(err)
	}
	go child.readStdout(strings.NewReader(string(requestLine) + "\n"))

	responseDone := make(chan jsonrpc.Response, 1)
	go func() {
		var response jsonrpc.Response
		_ = json.NewDecoder(childReader).Decode(&response)
		responseDone <- response
	}()
	select {
	case childResponse := <-responseDone:
		if childResponse.ID != childRequest.ID || childResponse.Error != nil {
			t.Fatalf("unexpected child response: %#v", childResponse)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for child response")
	}
}
