package runtime

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestMCPCanonicalNameAndRisk(t *testing.T) {
	if got := mcpCanonicalName("filesystem", "read.file"); got != "mcp__filesystem__read_file" {
		t.Fatalf("canonical = %q", got)
	}
	cfg := mcp.MCPServerConfig{
		Name:          "fake",
		RiskOverrides: map[string]string{"read_file": "low"},
	}
	if mcpToolRisk(cfg, "read_file") != tools.RiskLow {
		t.Fatal("expected low risk override")
	}
	if mcpToolRisk(cfg, "other") != tools.RiskHigh {
		t.Fatal("expected default high risk")
	}
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
			MCPServers: []mcp.MCPServerConfig{config},
		},
	}
	defs := rt.prepareMCPToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def, got %#v", defs)
	}
	if defs[0].Name != "mcp__fake__read_file" {
		t.Fatalf("unexpected tool name %q", defs[0].Name)
	}
	if defs[0].Risk != tools.RiskHigh {
		t.Fatalf("expected high risk, got %s", defs[0].Risk)
	}

	// toolsForReply should merge MCP defs without re-discovery when bindings exist.
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

	out, err := rt.executeMCPTool(context.Background(), ToolRunContext{
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
	waitForFile(t, cleanup)
	rt.clearMCPBindings(params.RunID)
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
			MCPServers: []mcp.MCPServerConfig{config},
		},
	}
	defs := rt.prepareMCPToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("discover for call-timeout mode should still list tools, got %#v", defs)
	}
	_, err := rt.executeMCPTool(context.Background(), ToolRunContext{RunID: params.RunID}, tools.Call{
		Name:      "mcp__fake__read_file",
		Arguments: map[string]any{},
	})
	if err == nil || !strings.Contains(err.Error(), "tools/call failed") {
		t.Fatalf("expected tools/call timeout, got %v", err)
	}
	waitForFile(t, cleanup)
}

func TestMCPCallIsError(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "call-error", cleanup)
	config.Timeouts.CallMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID: "run_mcp_err",
		Options: methods.ReplyOptions{
			MCPServers: []mcp.MCPServerConfig{config},
		},
	}
	_ = rt.prepareMCPToolsForRun(context.Background(), params)
	_, err := rt.executeMCPTool(context.Background(), ToolRunContext{RunID: params.RunID}, tools.Call{
		Name: "mcp__fake__read_file",
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied by server") {
		t.Fatalf("expected isError tool failure, got %v", err)
	}
	waitForFile(t, cleanup)
}

func TestInvocationFromCallAcceptsMCPExtraDefs(t *testing.T) {
	runner := ToolRunner{}
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

func TestDispatchMCPToolViaRunner(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "success", cleanup)
	config.Timeouts.CallMS = 1000
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	params := methods.ReplyParams{
		RunID:   "run_dispatch",
		Options: methods.ReplyOptions{MCPServers: []mcp.MCPServerConfig{config}},
	}
	_ = rt.prepareMCPToolsForRun(context.Background(), params)
	result, _ := rt.tools.RunWithContext(context.Background(), ToolRunContext{RunID: params.RunID}, ToolInvocation{
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
	waitForFile(t, cleanup)
}
