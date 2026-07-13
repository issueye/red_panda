package runtimeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
)

func TestRuntimeClientHelperProcess(t *testing.T) {
	if os.Getenv("RED_PANDA_RUNTIMECLIENT_HELPER") != "1" {
		return
	}
	os.Exit(7)
}

func TestClientSynthesizesTerminalEventOnUnexpectedProcessExit(t *testing.T) {
	received := make(chan events.Envelope, 1)
	client := New("", nil, "test", func(event events.Envelope) {
		received <- event
	}, nil)
	cmd := exec.Command(os.Args[0], "-test.run=TestRuntimeClientHelperProcess")
	cmd.Env = append(os.Environ(), "RED_PANDA_RUNTIMECLIENT_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client.cmd = cmd
	client.running = true
	client.runID = "run_crashed"
	client.sessionID = "session_crashed"
	client.wait(cmd)

	select {
	case event := <-received:
		if event.Type != events.EventError || event.RootRunID != "run_crashed" || event.Payload["status"] != "failed" {
			t.Fatalf("unexpected synthetic event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("unexpected process exit did not emit a terminal event")
	}
}

func TestClientReadStdoutAcceptsLargeToolEvent(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()

	received := make(chan events.Envelope, 1)
	client := New("", nil, "test", func(event events.Envelope) {
		received <- event
	}, nil)
	go client.readStdout(reader)

	env := events.Envelope{
		ProtocolVersion: events.ProtocolVersion,
		EventID:         "evt_large_tool_output",
		RootRunID:       "run_large",
		RunID:           "run_large",
		SessionID:       "session_large",
		RootSeq:         1,
		AgentSeq:        1,
		Agent: events.AgentRef{
			AgentID: "root",
			Role:    events.AgentRoleRoot,
			Path:    []string{"root"},
		},
		Type: events.EventToolFinished,
		Payload: map[string]any{
			"tool_call_id": "tool_large",
			"tool_name":    "workspace.read_file",
			"status":       "completed",
			"output":       strings.Repeat("x", 160*1024),
		},
	}
	note, err := jsonrpc.NewNotification(methods.AgentEvent, env)
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
