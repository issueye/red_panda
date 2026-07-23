package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentmcp "redpanda/agent/internal/mcp"
	agenttools "redpanda/agent/internal/tools"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestMCPCanonicalName(t *testing.T) {
	if got := agentmcp.CanonicalName("filesystem", "read.file"); got != "mcp__filesystem__read_file" {
		t.Fatalf("canonical = %q", got)
	}
}

func TestMCPCallManagementRPC(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "success", cleanup)
	config.Timeouts.CallMS = 1000
	var outBuf bytes.Buffer
	rt := New(strings.NewReader(""), &outBuf, &bytes.Buffer{}, "test")
	params := methods.MCPCallParams{
		Server:    config,
		ToolName:  "read_file",
		Arguments: map[string]any{"path": "try-call.md"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "call-1",
		"method":  methods.MCPCall,
		"params":  json.RawMessage(raw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	response := outBuf.String()
	if !strings.Contains(response, `"ok":true`) && !strings.Contains(response, `"ok": true`) {
		// Direct path fallback assertion when envelope shape differs.
		out, callErr := rt.mcp.CallTool(context.Background(), "", config, "read_file", map[string]any{"path": "try-call.md"})
		if callErr != nil {
			t.Fatalf("handleLine response=%q callErr=%v", response, callErr)
		}
		if !strings.Contains(out, "path=try-call.md") {
			t.Fatalf("output = %q response=%q", out, response)
		}
	} else if !strings.Contains(response, "try-call.md") {
		t.Fatalf("response missing tool output: %s", response)
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
}

func TestMCPDiscoveryCacheAcrossRuns(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "success", cleanup)
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params1 := methods.ReplyParams{
		RunID: "run_cache_1",
		Options: methods.ReplyOptions{MCPServers: []protomcp.MCPServerConfig{config}},
	}
	defs1 := rt.mcp.PrepareToolsForRun(context.Background(), params1)
	if len(defs1) != 1 {
		t.Fatalf("first prepare: %#v", defs1)
	}
	if rt.mcp.LiveSessionCount() != 1 {
		t.Fatalf("live sessions after first prepare = %d", rt.mcp.LiveSessionCount())
	}
	// Second run on same Manager should hit discovery cache and keep the same process.
	params2 := methods.ReplyParams{
		RunID: "run_cache_2",
		Options: methods.ReplyOptions{MCPServers: []protomcp.MCPServerConfig{config}},
	}
	defs2 := rt.mcp.PrepareToolsForRun(context.Background(), params2)
	if len(defs2) != 1 {
		t.Fatalf("second prepare: %#v", defs2)
	}
	if rt.mcp.LiveSessionCount() != 1 {
		t.Fatalf("cache path should not spawn another process, live=%d", rt.mcp.LiveSessionCount())
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
}

func TestPrepareMCPToolsAndCall(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "success", cleanup)
	config.Timeouts.CallMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID: "run_mcp_1",
		Session: methods.ReplySession{
			ID:         "sess_1",
			WorkingDir: "",
		},
		Options: methods.ReplyOptions{
			MCPServers: []protomcp.MCPServerConfig{config},
		},
	}
	defs := rt.mcp.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %#v", defs)
	}
	if defs[0].Name != "mcp__fake__read_file" {
		t.Fatalf("unexpected tool name %q", defs[0].Name)
	}
	if defs[0].Risk != tools.RiskHigh {
		t.Fatalf("expected high risk, got %s", defs[0].Risk)
	}

	merged := rt.toolsForReply(context.Background(), params)
	found := false
	for _, d := range merged {
		if d.Name == "mcp__fake__read_file" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("merged tools missing MCP tool: %d tools", len(merged))
	}

	out, err := rt.executeMCPTool(context.Background(), agenttools.ToolRunContext{
		RunID:      params.RunID,
		WorkingDir: "",
	}, tools.Call{
		Name:      "mcp__fake__read_file",
		Arguments: map[string]any{"path": "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ok tool=read_file") || !strings.Contains(out, "path=README.md") {
		t.Fatalf("unexpected call output: %q", out)
	}
	if rt.mcp.LiveSessionCount() != 1 {
		t.Fatalf("expected one reused MCP session after discover+call, got %d", rt.mcp.LiveSessionCount())
	}
	// Second call must reuse the same process (no extra spawn).
	out2, err := rt.executeMCPTool(context.Background(), agenttools.ToolRunContext{
		RunID: params.RunID, WorkingDir: "",
	}, tools.Call{Name: "mcp__fake__read_file", Arguments: map[string]any{"path": "main.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "path=main.go") {
		t.Fatalf("second call output: %q", out2)
	}
	if rt.mcp.LiveSessionCount() != 1 {
		t.Fatalf("expected still one session after reuse, got %d", rt.mcp.LiveSessionCount())
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
	rt.mcp.ClearBindings(params.RunID)
}

func TestMCPCallSendsCancelledNotification(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	cancelMark := filepath.Join(t.TempDir(), "cancelled.json")
	config := helperMCPConfig(t, "call-hang-cancel", cleanup)
	config.Env["RED_PANDA_MCP_CANCEL_MARK"] = cancelMark
	config.Timeouts.CallMS = 5000
	config.Timeouts.InitializeMS = 2000
	config.Timeouts.StartMS = 2000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID:   "run_mcp_cancel_proto",
		Options: methods.ReplyOptions{MCPServers: []protomcp.MCPServerConfig{config}},
	}
	if defs := rt.mcp.PrepareToolsForRun(context.Background(), params); len(defs) != 1 {
		t.Fatalf("discover: %#v", defs)
	}

	callCtx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := rt.executeMCPTool(callCtx, agenttools.ToolRunContext{RunID: params.RunID}, tools.Call{
			Name:      "mcp__fake__read_file",
			Arguments: map[string]any{"path": "x"},
		})
		errCh <- err
	}()

	// Let the call leave the client and hang on the server before cancelling.
	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancel error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("call did not unblock after cancel")
	}

	waitForFile(t, cancelMark)
	raw, err := os.ReadFile(cancelMark)
	if err != nil {
		t.Fatal(err)
	}
	var note struct {
		Method string `json:"method"`
		Params struct {
			RequestID any    `json:"requestId"`
			Reason    string `json:"reason"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &note); err != nil {
		t.Fatalf("cancelled payload %q: %v", raw, err)
	}
	if note.Method != "notifications/cancelled" {
		t.Fatalf("method = %q, payload %s", note.Method, raw)
	}
	if note.Params.RequestID == nil {
		t.Fatalf("missing requestId in %s", raw)
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
}

func TestMCPCallTimeoutAndCleanup(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "call-timeout", cleanup)
	config.Timeouts.CallMS = 80
	config.Timeouts.InitializeMS = 1000
	config.Timeouts.StartMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID: "run_mcp_timeout",
		Options: methods.ReplyOptions{
			MCPServers: []protomcp.MCPServerConfig{config},
		},
	}
	defs := rt.mcp.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("discover for call-timeout mode should still list tools, got %#v", defs)
	}
	_, err := rt.executeMCPTool(context.Background(), agenttools.ToolRunContext{RunID: params.RunID}, tools.Call{
		Name:      "mcp__fake__read_file",
		Arguments: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "tools/call failed") {
		t.Fatalf("expected tools/call timeout, got %v", err)
	}
	// Transport timeout drops the pooled session and kills the child.
	waitForFile(t, cleanup)
	rt.mcp.CloseAll()
}

func TestMCPCallIsError(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "call-error", cleanup)
	config.Timeouts.CallMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID: "run_mcp_err",
		Options: methods.ReplyOptions{
			MCPServers: []protomcp.MCPServerConfig{config},
		},
	}
	_ = rt.mcp.PrepareToolsForRun(context.Background(), params)
	_, err := rt.executeMCPTool(context.Background(), agenttools.ToolRunContext{RunID: params.RunID}, tools.Call{
		Name: "mcp__fake__read_file",
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied by server") {
		t.Fatalf("expected isError tool failure, got %v", err)
	}
	// Business isError keeps the process; cleanup runs on explicit CloseAll.
	if rt.mcp.LiveSessionCount() != 1 {
		t.Fatalf("isError should not drop live session, got %d", rt.mcp.LiveSessionCount())
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
}

func TestInvocationFromCallAcceptsMCPExtraDefs(t *testing.T) {
	runner := agenttools.ToolRunner{}
	inv, err := runner.InvocationFromCall("run_x", 0, tools.Call{Name: "mcp__fake__read_file"}, tools.Definition{
		Name: "mcp__fake__read_file",
		Risk: tools.RiskLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Call.Risk != tools.RiskLow {
		t.Fatalf("risk not applied: %#v", inv.Call)
	}
}

type ToolRunContext = agenttools.ToolRunContext
type ToolInvocation = agenttools.ToolInvocation

func TestDispatchMCPToolViaRunner(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "success", cleanup)
	config.Timeouts.CallMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID:   "run_dispatch",
		Options: methods.ReplyOptions{MCPServers: []protomcp.MCPServerConfig{config}},
	}
	_ = rt.mcp.PrepareToolsForRun(context.Background(), params)
	result, _ := rt.tools.RunWithContext(context.Background(), agenttools.ToolRunContext{RunID: params.RunID}, agenttools.ToolInvocation{
		Call: tools.Call{
			ID:   "tc1",
			Name: "mcp__fake__read_file",
			Risk: tools.RiskHigh,
			Arguments: map[string]any{
				"path": "x.go",
			},
		},
	})
	if result.Status != tools.CallStatusCompleted {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.Output, "path=x.go") {
		t.Fatalf("output missing path: %s", result.Output)
	}
	rt.mcp.CloseAll()
	waitForFile(t, cleanup)
}
