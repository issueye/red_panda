package runtimeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
)

func TestClientReadStdoutAcceptsLargeToolEvent(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()

	received := make(chan events.EnvelopeV2, 1)
	client := New("", nil, "test", func(event events.EnvelopeV2) {
		received <- event
	}, nil)
	go client.readStdout(reader)

	env := events.EnvelopeV2{
		ProtocolVersion: events.ProtocolVersionV2,
		EventID:         "evt_large_tool_output",
		RunID:           "run_large",
		SessionID:       "session_large",
		AssignmentID:    "assignment_large",
		RunSeq:          1,
		WorkerSeq:       1,
		Worker:          events.EventWorkerRef{ID: "worker-01"},
		Type:            events.EventToolFinished,
		Payload: map[string]any{
			"tool_call_id": "tool_large",
			"tool_name":    "workspace.read_file",
			"status":       "completed",
			"output":       strings.Repeat("x", 160*1024),
		},
	}
	note, err := jsonrpc.NewNotification(methods.RunEvent, env)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 64*1024 {
		t.Fatalf("test event is not large enough: %d bytes", len(raw))
	}
	go func() {
		_, _ = fmt.Fprintln(writer, string(raw))
		_ = writer.Close()
	}()

	select {
	case got := <-received:
		if got.EventID != env.EventID || got.Type != events.EventToolFinished {
			t.Fatalf("unexpected event: %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("large runtime event was not delivered")
	}
}

func TestClientHandlesRuntimeOriginatedRequest(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != methods.StateToolExecute {
			t.Fatalf("method = %s, want %s", method, methods.StateToolExecute)
		}
		var req methods.StateToolExecuteParams
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

	request, err := jsonrpc.NewRequest("rt_1", methods.StateToolExecute, methods.NewStateToolParams(
		methods.StateToolDomainMemory, "run_1", "", "", "tool_1", "memory.list", nil,
	))
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
			var result any = methods.InitializeResult{ProtocolVersion: events.ProtocolVersionV2}
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

func TestClientUsesV2RunAndWorkerMethods(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, nil)
	client.running = true
	client.stdin = writer

	expected := []string{
		methods.RunExecute,
		methods.RunCancel,
		methods.WorkerList,
		methods.WorkerAssignmentCancel,
		methods.WorkerMessageSend,
		methods.WorkerMessageReceive,
		methods.WorkerPoolStatus,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		decoder := json.NewDecoder(reader)
		for _, targetMethod := range expected {
			for _, wantMethod := range []string{methods.CoreInitialize, targetMethod} {
				var req jsonrpc.Request
				if err := decoder.Decode(&req); err != nil {
					t.Errorf("decode %s request: %v", wantMethod, err)
					return
				}
				if req.Method != wantMethod {
					t.Errorf("method = %q, want %q", req.Method, wantMethod)
					return
				}
				var result any = methods.InitializeResult{ProtocolVersion: events.ProtocolVersionV2}
				switch req.Method {
				case methods.CoreInitialize:
					var params methods.InitializeParams
					if err := json.Unmarshal(req.Params, &params); err != nil {
						t.Errorf("decode initialize params: %v", err)
						return
					}
					if params.ProtocolVersion != events.ProtocolVersionV2 {
						t.Errorf("protocol version = %q", params.ProtocolVersion)
						return
					}
				case methods.RunExecute:
					result = methods.RunExecuteResult{Accepted: true, RunID: "run_1", AssignmentID: "assignment_1", WorkerID: "worker_1"}
				case methods.RunCancel:
					result = methods.RunCancelResult{Accepted: true, RunID: "run_1", Cancelled: 1}
				case methods.WorkerList:
					result = methods.WorkerListResult{}
				case methods.WorkerAssignmentCancel:
					result = methods.WorkerAssignmentCancelResult{Accepted: true, RunID: "run_1", AssignmentID: "assignment_1", Cancelled: true}
				case methods.WorkerMessageSend:
					result = methods.WorkerMessageSendResult{Accepted: true}
				case methods.WorkerMessageReceive:
					result = methods.WorkerMessageReceiveResult{Found: false}
				case methods.WorkerPoolStatus:
					result = methods.WorkerPoolStatusResult{Pool: methods.PoolSnapshot{Configured: 4, Ready: 4}}
				}
				resp, _ := jsonrpc.NewResult(req.ID, result)
				raw, _ := json.Marshal(resp)
				_ = client.handleGatewayLikeResponseForTest(raw)
			}
		}
	}()

	ctx := context.Background()
	if _, err := client.Execute(ctx, methods.RunExecuteParams{RunID: "run_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelRun(ctx, methods.RunCancelParams{RunID: "run_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Workers(ctx, methods.WorkerListParams{RunID: "run_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelAssignment(ctx, methods.WorkerAssignmentCancelParams{RunID: "run_1", AssignmentID: "assignment_1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendWorkerMessage(ctx, methods.WorkerMessageSendParams{ToWorkerID: "worker_2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReceiveWorkerMessage(ctx, methods.WorkerMessageReceiveParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WorkerPoolStatus(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestClientRejectsRuntimeProtocolV1(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, nil)
	client.running = true
	client.stdin = writer
	go func() {
		var req jsonrpc.Request
		_ = json.NewDecoder(reader).Decode(&req)
		resp, _ := jsonrpc.NewResult(req.ID, methods.InitializeResult{ProtocolVersion: "2026-07-09"})
		raw, _ := json.Marshal(resp)
		_ = client.handleGatewayLikeResponseForTest(raw)
	}()

	err := client.Initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("Initialize error = %v, want incompatible protocol", err)
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

// stubSleepCommand returns a shell command that stays alive for the given
// duration without responding, mimicking a runtime process that has not exited.
func stubSleepCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", "Start-Sleep -Seconds 10"}
	}
	return "sleep", []string{"10"}
}

// TestRuntimeProcessSurvivesRequestCancellation guards against regressing back
// to exec.CommandContext(requestCtx): the managed runtime is a long-lived
// single-core process and must not be killed when the HTTP request that started
// it returns. Skills management (and other out-of-run calls) issue one-shot
// requests whose context is cancelled as soon as the handler returns.
func TestRuntimeProcessSurvivesRequestCancellation(t *testing.T) {
	// Use stdio transport: the sleep stub never dials IPC. The invariant under
	// test is process lifetime vs request context, independent of transport.
	t.Setenv("RED_PANDA_RUNTIME_IPC", "stdio")

	command, args := stubSleepCommand()
	client := New(command, args, "test", nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := client.ensureStarted(ctx); err != nil {
		t.Fatalf("ensureStarted: %v", err)
	}

	client.mu.Lock()
	cmd := client.cmd
	running := client.running
	client.mu.Unlock()
	if cmd == nil || cmd.Process == nil || !running {
		t.Fatalf("runtime process not started: cmd=%v running=%v", cmd, running)
	}

	// Simulate the HTTP handler returning and cancelling its request context.
	cancel()
	time.Sleep(100 * time.Millisecond)

	client.mu.Lock()
	stillRunning := client.running
	process := client.cmd.Process
	client.mu.Unlock()
	if !stillRunning {
		t.Fatal("runtime process was killed when the request context was cancelled; use exec.Command, not exec.CommandContext(reqCtx)")
	}
	if process != nil {
		_ = process.Kill()
	}
	_ = client.Shutdown(context.Background())
}
