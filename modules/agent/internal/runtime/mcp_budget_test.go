package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/protocol/methods"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/tools"
)

// managerFromFakeServer constructs an MCP Manager and feeds it a ReplyParams
// whose MCPServers entry points at the fake helper process in the given mode.
// Each invocation models one run's PrepareToolsForRun pass.
func managerFromFakeServer(t *testing.T, mode, cleanupPath string) (*agentmcp.Manager, methods.ReplyParams) {
	t.Helper()
	cfg := helperMCPConfig(t, mode, cleanupPath)
	mgr := agentmcp.NewManager("test", &bytes.Buffer{})
	params := methods.ReplyParams{
		RunID: "run_test",
		Session: methods.ReplySession{ID: "session_test", WorkingDir: ""},
		Options: methods.ReplyOptions{
			MCPServers: []protomcp.MCPServerConfig{cfg},
		},
	}
	return mgr, params
}

func TestMCPManagerDisablesServerAfterCrashLimit(t *testing.T) {
	// Set a tight, deterministic budget via env. NewManager reads env at construct time.
	t.Setenv("RED_PANDA_MCP_CRASH_LIMIT", "2")
	t.Setenv("RED_PANDA_MCP_CRASH_WINDOW_MS", "60000")

	cleanup := filepath.Join(t.TempDir(), "cleanup")
	mgr, params := managerFromFakeServer(t, "exit-immediately", cleanup)

	// First discovery attempt: server crashes → 1 startup-phase failure.
	defs := mgr.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 0 {
		t.Fatalf("first prepare: expected 0 defs from crashed server, got %d", len(defs))
	}
	if disabled := mgr.DisabledServers(); len(disabled) != 0 {
		t.Fatalf("after 1 failure: expected no disabled servers, got %v", disabled)
	}

	// Second discovery: 2nd failure → budget threshold (limit=2) hit → disabled.
	defs = mgr.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 0 {
		t.Fatalf("second prepare: expected 0 defs, got %d", len(defs))
	}
	disabled := mgr.DisabledServers()
	if len(disabled) != 1 || disabled[0] != "fake" {
		t.Fatalf("after 2 failures: expected [fake] disabled, got %v", disabled)
	}

	// Third discovery: already disabled → should NOT spawn a new process.
	// Assert via cleanup marker: write a fresh marker file path the helper would
	// touch on spawn; if disabled, the file must not appear.
	cleanup2 := filepath.Join(t.TempDir(), "cleanup2")
	params.Options.MCPServers[0].Env["RED_PANDA_MCP_CLEANUP"] = cleanup2
	defs = mgr.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 0 {
		t.Fatalf("third prepare: disabled server must yield 0 defs, got %d", len(defs))
	}
	// Give the OS a brief window in case a stray spawn raced us.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cleanup2); err == nil {
			t.Fatalf("disabled server should not spawn a process, but cleanup marker was written: %s", cleanup2)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMCPManagerBudgetDisabledByEnvLimit0(t *testing.T) {
	t.Setenv("RED_PANDA_MCP_CRASH_LIMIT", "0")
	t.Setenv("RED_PANDA_MCP_CRASH_WINDOW_MS", "60000")

	cleanup := filepath.Join(t.TempDir(), "cleanup")
	mgr, params := managerFromFakeServer(t, "exit-immediately", cleanup)

	for i := 0; i < 5; i++ {
		_ = mgr.PrepareToolsForRun(context.Background(), params)
	}
	if disabled := mgr.DisabledServers(); len(disabled) != 0 {
		t.Fatalf("limit=0 must disable the budget entirely, got %v", disabled)
	}
}

func TestMCPManagerBusinessCallFailureNotCounted(t *testing.T) {
	t.Setenv("RED_PANDA_MCP_CRASH_LIMIT", "2")
	t.Setenv("RED_PANDA_MCP_CRASH_WINDOW_MS", "60000")

	// mode=call-error: server starts fine (initialize succeeds), tools/call
	// returns isError → ExecuteTool fails with a "tools/call failed:"-style
	// error path that must NOT be counted by the budget.
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	mgr, params := managerFromFakeServer(t, "call-error", cleanup)

	// First prepare must register a binding so we can ExecuteTool.
	defs := mgr.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def from call-error server, got %d", len(defs))
	}

	for i := 0; i < 3; i++ {
		_, err := mgr.ExecuteTool(context.Background(), params.RunID, "", tools.Call{
			Name:      defs[0].Name,
			Arguments: map[string]any{"path": "x"},
		})
		if err == nil {
			t.Fatalf("call-error server should return error on iteration %d", i)
		}
	}
	if disabled := mgr.DisabledServers(); len(disabled) != 0 {
		t.Fatalf("business call failures must not trigger crash budget, got %v", disabled)
	}
}

func TestMCPManagerExecuteToolRejectsDisabledServer(t *testing.T) {
	t.Setenv("RED_PANDA_MCP_CRASH_LIMIT", "2")
	t.Setenv("RED_PANDA_MCP_CRASH_WINDOW_MS", "60000")

	// Register a healthy binding first, then disable the server by simulating
	// startup-phase failures via a different fake config (exit-immediately) with
	// the same Name. This models "server was healthy at run start, broke later".
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	mgr, params := managerFromFakeServer(t, "success", cleanup)
	defs := mgr.PrepareToolsForRun(context.Background(), params)
	if len(defs) != 1 {
		t.Fatalf("expected 1 def from healthy server, got %d", len(defs))
	}

	// Drive two startup-phase failures (through PrepareToolsForRun) to disable.
	disableParams := params
	disableParams.Options.MCPServers[0].Args = []string{"-test.run=TestMCPHelperProcess", "--", "exit-immediately"}
	disableParams.RunID = "run_disable" // avoid clobbering the healthy binding bucket
	mgr.PrepareToolsForRun(context.Background(), disableParams)
	mgr.PrepareToolsForRun(context.Background(), disableParams)
	if disabled := mgr.DisabledServers(); len(disabled) != 1 || disabled[0] != "fake" {
		t.Fatalf("expected [fake] disabled after 2 crashes, got %v", disabled)
	}

	// Now ExecuteTool against the original binding must be rejected without
	// spawning a subprocess. The error message must mention "disabled by crash budget".
	_, err := mgr.ExecuteTool(context.Background(), params.RunID, "", tools.Call{
		Name:      defs[0].Name,
		Arguments: map[string]any{"path": "x"},
	})
	if err == nil {
		t.Fatalf("ExecuteTool against disabled server must error")
	}
	if !strings.Contains(err.Error(), "disabled by crash budget") {
		t.Fatalf("expected disabled-by-budget error, got %v", err)
	}
}
