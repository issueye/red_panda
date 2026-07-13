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

func TestSubAgentProcessCriticalEventDoesNotBlockRPCReader(t *testing.T) {
	process := &subAgentProcess{
		events: make(chan events.Envelope, 1),
		done:   make(chan struct{}),
	}
	process.startEventDispatcher()
	for i := 0; i < cap(process.events); i++ {
		process.events <- events.Envelope{Type: events.EventReasoningDelta}
	}

	delivered := make(chan struct{})
	go func() {
		process.enqueueEvent(events.Envelope{Type: events.EventToolFinished})
		close(delivered)
	}()

	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("critical event blocked the RPC reader")
	}

	<-process.events
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		event := <-process.events
		if event.Type == events.EventToolFinished {
			select {
			case duplicate := <-process.events:
				if duplicate.Type == events.EventToolFinished {
					t.Fatal("critical event was delivered more than once")
				}
			case <-time.After(20 * time.Millisecond):
			}
			return
		}
	}
	t.Fatal("critical event was not delivered after queue space became available")
}

func TestSubAgentProcessCancelDeadlineIgnoresSaturatedEventQueue(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		_, _ = io.Copy(io.Discard, reader)
	}()
	process := &subAgentProcess{
		running: true,
		stdin:   writer,
		pending: map[jsonrpc.ID]chan jsonrpc.Response{},
		events:  make(chan events.Envelope, 1),
		done:    make(chan struct{}),
	}
	process.startEventDispatcher()
	process.events <- events.Envelope{Type: events.EventReasoningDelta}
	process.enqueueEvent(events.Envelope{Type: events.EventFinish})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := process.Cancel(ctx, "run_cancel_deadline", "test")
	if err == nil {
		t.Fatal("cancel unexpectedly succeeded without a Runtime response")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("cancel exceeded bounded deadline: %s", elapsed)
	}
	process.mu.Lock()
	running := process.running
	process.mu.Unlock()
	if running {
		t.Fatal("timed-out cancel did not force the worker transport closed")
	}
	close(process.done)
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
