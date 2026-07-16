package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/jsonrpc"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
)

func TestMCPDiscoverSuccessAndBoundedStderr(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "stderr", cleanup)
	result := runMCPDiscover(t, config)
	if result.Status != "ready" {
		t.Fatalf("expected ready discovery, got %#v", result)
	}
	if result.ServerInfo.Name != "fake-mcp" || result.ServerInfo.Version != "1.0.0" {
		t.Fatalf("unexpected server info: %#v", result.ServerInfo)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "read_file" {
		t.Fatalf("unexpected tools: %#v", result.Tools)
	}
	const stderrLimit = 8192
	if len(result.StderrSummary) != stderrLimit {
		t.Fatalf("stderr summary length = %d, want %d", len(result.StderrSummary), stderrLimit)
	}
	waitForFile(t, cleanup)
}

func TestMCPDiscoverRedactsEnvironmentSecretsFromStderr(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "secret-stderr", cleanup)
	config.Env["MCP_API_KEY"] = "super-secret-token"
	result := runMCPDiscover(t, config)
	if result.Status != "ready" {
		t.Fatalf("expected ready discovery, got %#v", result)
	}
	if strings.Contains(result.StderrSummary, "super-secret-token") {
		t.Fatalf("stderr leaked configured secret: %q", result.StderrSummary)
	}
	if !strings.Contains(result.StderrSummary, "diagnostic=****") {
		t.Fatalf("stderr did not contain redaction marker: %q", result.StderrSummary)
	}
}

func TestMCPDiscoverRedactsEnvironmentSecretsFromError(t *testing.T) {
	config := helperMCPConfig(t, "secret-error", filepath.Join(t.TempDir(), "cleanup"))
	config.Env["MCP_API_KEY"] = "super-secret-token"
	result := runMCPDiscover(t, config)
	if result.Status != "failed" || strings.Contains(result.Error, "super-secret-token") {
		t.Fatalf("discovery error leaked configured secret: %#v", result)
	}
	if !strings.Contains(result.Error, "rejected ****") {
		t.Fatalf("discovery error did not contain redaction marker: %q", result.Error)
	}
}

func TestMCPDiscoverAppliesToolAllowlist(t *testing.T) {
	config := helperMCPConfig(t, "multiple-tools", filepath.Join(t.TempDir(), "cleanup-all"))
	all := runMCPDiscover(t, config)
	if len(all.Tools) != 2 {
		t.Fatalf("empty allowlist returned %d tools, want 2: %#v", len(all.Tools), all.Tools)
	}

	config = helperMCPConfig(t, "multiple-tools", filepath.Join(t.TempDir(), "cleanup-filtered"))
	config.ToolAllowlist = []string{"write_file"}
	filtered := runMCPDiscover(t, config)
	if len(filtered.Tools) != 1 || filtered.Tools[0].Name != "write_file" {
		t.Fatalf("unexpected allowlisted tools: %#v", filtered.Tools)
	}
}

func TestMCPDiscoverRejectsInvalidStdoutAndCleansUp(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "invalid", cleanup)
	// mcp-go silently skips non-JSON stdout lines, so initialize fails via
	// timeout / transport close rather than a dedicated parse error.
	config.Timeouts.InitializeMS = 200
	result := runMCPDiscover(t, config)
	if result.Status != "failed" {
		t.Fatalf("expected failed discovery for invalid stdout, got %#v", result)
	}
	if !strings.Contains(result.Error, "initialize failed") {
		t.Fatalf("expected initialize failure, got %#v", result)
	}
	waitForFile(t, cleanup)
}

func TestMCPDiscoverTimesOutAndCleansUp(t *testing.T) {
	for _, test := range []struct {
		name  string
		mode  string
		phase string
	}{
		{name: "initialize", mode: "timeout-initialize", phase: "initialize failed: timeout"},
		{name: "tools list", mode: "timeout", phase: "tools/list failed: timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cleanup := filepath.Join(t.TempDir(), "cleanup")
			config := helperMCPConfig(t, test.mode, cleanup)
			if test.mode == "timeout-initialize" {
				config.Timeouts.InitializeMS = 100
			} else {
				config.Timeouts.ListMS = 100
			}
			started := time.Now()
			result := runMCPDiscover(t, config)
			if result.Status != "failed" || !strings.Contains(result.Error, test.phase) {
				t.Fatalf("expected %s, got %#v", test.phase, result)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("timeout cleanup took too long: %s", time.Since(started))
			}
			waitForFile(t, cleanup)
		})
	}
}

func TestMCPDiscoverReportsProcessExit(t *testing.T) {
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	result := runMCPDiscover(t, helperMCPConfig(t, "exit", cleanup))
	if result.Status != "failed" || !strings.Contains(result.Error, "process exited") {
		t.Fatalf("expected process exit failure, got %#v", result)
	}
	waitForFile(t, cleanup)
}

func TestMCPDiscoverHandleLine(t *testing.T) {
	var output bytes.Buffer
	runtime := New(strings.NewReader(""), &output, &bytes.Buffer{}, "test")
	config := helperMCPConfig(t, "success", filepath.Join(t.TempDir(), "cleanup"))
	params := methods.MCPDiscoverParams{Servers: []protomcp.MCPServerConfig{config}}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "discover-1", "method": methods.MCPDiscover, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.handleLine(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var response jsonrpc.Response
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result protomcp.MCPDiscoveryResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Servers) != 1 || result.Servers[0].Status != "ready" {
		t.Fatalf("unexpected discovery response: %#v", result)
	}
}

func TestCoreShutdownClosesActiveMCPProcess(t *testing.T) {
	runtime := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	cleanup := filepath.Join(t.TempDir(), "cleanup")
	config := helperMCPConfig(t, "timeout", cleanup)
	config.Timeouts.ListMS = 5000
	done := make(chan protomcp.MCPServerDiscovery, 1)
	go func() { done <- runtime.mcp.DiscoverServer(context.Background(), "", config) }()
	waitForFile(t, cleanup+".ready")
	runtime.mcp.CloseAll()
	select {
	case result := <-done:
		if result.Status != "failed" {
			t.Fatalf("expected interrupted discovery, got %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not stop after shutdown")
	}
	waitForFile(t, cleanup)
}

func helperMCPConfig(t *testing.T, mode, cleanup string) protomcp.MCPServerConfig {
	t.Helper()
	return protomcp.MCPServerConfig{
		Name:    "fake",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPHelperProcess", "--", mode},
		Env: map[string]string{
			"RED_PANDA_MCP_HELPER":  "1",
			"RED_PANDA_MCP_CLEANUP": cleanup,
		},
		Enabled: true,
		Timeouts: protomcp.MCPTimeouts{
			StartMS: 1000, InitializeMS: 1000, ListMS: 1000, ShutdownMS: 200,
		},
	}
}

func runMCPDiscover(t *testing.T, config protomcp.MCPServerConfig) protomcp.MCPServerDiscovery {
	t.Helper()
	rt := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, "test")
	return rt.mcp.DiscoverServer(context.Background(), "", config)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cleanup marker was not written: %s", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("RED_PANDA_MCP_HELPER") != "1" {
		return
	}
	if path := os.Getenv("RED_PANDA_MCP_CLEANUP"); path != "" {
		_ = os.WriteFile(path+".ready", []byte("ready"), 0o600)
	}
	mode := "success"
	for index, arg := range os.Args {
		if arg == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
		}
	}
	if mode == "stderr" {
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("x", 8192*2))
	}
	if mode == "secret-stderr" {
		_, _ = fmt.Fprintf(os.Stderr, "diagnostic=%s", os.Getenv("MCP_API_KEY"))
	}
	// Flush stderr before serving so the client drain can observe diagnostics
	// even when initialize fails immediately.
	_ = os.Stderr.Sync()

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	writeResult := func(id json.RawMessage, result any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": jsonRawOrNull(id), "result": result})
	}
	writeError := func(id json.RawMessage, code int, message string) {
		_ = enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      jsonRawOrNull(id),
			"error":   map[string]any{"code": code, "message": message},
		})
	}

	scanner := bufio.NewScanner(os.Stdin)
	initialized := false
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		switch request.Method {
		case "initialize":
			if mode == "exit" {
				if path := os.Getenv("RED_PANDA_MCP_CLEANUP"); path != "" {
					_ = os.WriteFile(path, []byte("closed"), 0o600)
				}
				os.Exit(3)
			}
			if mode == "timeout-initialize" {
				continue
			}
			if mode == "secret-error" {
				writeError(request.ID, -32000, fmt.Sprintf("rejected %s", os.Getenv("MCP_API_KEY")))
				continue
			}
			if mode == "invalid" {
				fmt.Fprintln(os.Stdout, "not-json")
				continue
			}
			writeResult(request.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "1.0.0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "notifications/initialized":
			initialized = true
		case "tools/list":
			if mode == "timeout" {
				continue
			}
			if !initialized {
				writeError(request.ID, -32000, "not initialized")
				continue
			}
			if mode == "multiple-tools" {
				writeResult(request.ID, map[string]any{
					"tools": []map[string]any{
						{"name": "read_file", "description": "Read a file", "inputSchema": map[string]any{"type": "object"}},
						{"name": "write_file", "description": "Write a file", "inputSchema": map[string]any{"type": "object"}},
					},
				})
			} else {
				writeResult(request.ID, map[string]any{
					"tools": []map[string]any{
						{
							"name":        "read_file",
							"description": "Read a file",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
						},
					},
				})
			}
		case "tools/call":
			if mode == "call-timeout" {
				continue
			}
			if !initialized {
				writeError(request.ID, -32000, "not initialized")
				continue
			}
			if mode == "call-error" {
				writeResult(request.ID, map[string]any{
					"content": []map[string]any{{"type": "text", "text": "permission denied by server"}},
					"isError": true,
				})
				continue
			}
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(request.Params, &params)
			path, _ := params.Arguments["path"].(string)
			text := fmt.Sprintf("ok tool=%s path=%s", params.Name, path)
			writeResult(request.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": false,
			})
		}
	}
	if path := os.Getenv("RED_PANDA_MCP_CLEANUP"); path != "" {
		_ = os.WriteFile(path, []byte("closed"), 0o600)
	}
	os.Exit(0)
}

// jsonRawOrNull returns id for encoding; empty becomes null for notifications.
func jsonRawOrNull(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(id, &v); err != nil {
		return string(id)
	}
	return v
}
