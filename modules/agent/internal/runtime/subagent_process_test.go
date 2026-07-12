package runtime

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func TestSubAgentProcessProxiesGatewayRequestAndReturnsResponse(t *testing.T) {
	gatewayReader, gatewayWriter := io.Pipe()
	defer gatewayReader.Close()
	defer gatewayWriter.Close()

	rt := New(strings.NewReader(""), gatewayWriter, io.Discard, "test")
	childReader, childWriter := io.Pipe()
	defer childReader.Close()
	defer childWriter.Close()
	child := &subAgentProcess{
		stdin:     childWriter,
		running:   true,
		onRequest: rt.callGateway,
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

	var gatewayRequest jsonrpc.Request
	if err := json.NewDecoder(gatewayReader).Decode(&gatewayRequest); err != nil {
		t.Fatal(err)
	}
	if gatewayRequest.Method != methods.MemoryToolExecute || gatewayRequest.ID == "" {
		t.Fatalf("unexpected proxied gateway request: %#v", gatewayRequest)
	}

	gatewayResponse, err := jsonrpc.NewResult(gatewayRequest.ID, methods.MemoryToolExecuteResult{
		Status:   "completed",
		Output:   `{"action":"memory.create"}`,
		RecordID: "mem_child_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	rawGatewayResponse, err := json.Marshal(gatewayResponse)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(context.Background(), rawGatewayResponse); err != nil {
		t.Fatal(err)
	}

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
		var result methods.MemoryToolExecuteResult
		if err := json.Unmarshal(childResponse.Result, &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "completed" || result.RecordID != "mem_child_1" {
			t.Fatalf("unexpected child result: %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for proxied child response")
	}
}

func TestSubAgentProcessCriticalEventWaitsForQueueSpace(t *testing.T) {
	process := &subAgentProcess{
		events: make(chan events.Envelope, 1),
		done:   make(chan struct{}),
	}
	process.events <- events.Envelope{Type: events.EventReasoningDelta}

	delivered := make(chan struct{})
	go func() {
		process.enqueueEvent(events.Envelope{Type: events.EventToolFinished})
		close(delivered)
	}()

	select {
	case <-delivered:
		t.Fatal("critical event was dropped instead of waiting for queue space")
	case <-time.After(20 * time.Millisecond):
	}

	<-process.events
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("critical event was not delivered after queue space became available")
	}
	if event := <-process.events; event.Type != events.EventToolFinished {
		t.Fatalf("event type = %s, want tool_finished", event.Type)
	}
}

func TestSubAgentProcessDropsOnlyHighFrequencyOptionalEvents(t *testing.T) {
	for _, typ := range []events.EventType{
		events.EventReasoningDelta,
		events.EventUsage,
		events.EventToolOutput,
	} {
		if !isDroppableSubAgentEvent(typ) {
			t.Fatalf("%s should be droppable", typ)
		}
	}
	for _, typ := range []events.EventType{
		events.EventToolStarted,
		events.EventToolFinished,
		events.EventToolFailed,
		events.EventMessageDelta,
		events.EventError,
		events.EventFinish,
	} {
		if isDroppableSubAgentEvent(typ) {
			t.Fatalf("%s must be delivered reliably", typ)
		}
	}
}
