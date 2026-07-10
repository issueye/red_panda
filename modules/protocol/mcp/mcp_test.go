package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMCPServerConfigJSONRoundTrip(t *testing.T) {
	want := MCPServerConfig{
		Name:    "filesystem",
		Command: "mcp-filesystem",
		Args:    []string{"--root", "."},
		Env:     map[string]string{"MODE": "readonly"},
		CWD:     "D:/workspace",
		Enabled: true,
		Timeouts: MCPTimeouts{
			StartMS:      10000,
			InitializeMS: 10000,
			ListMS:       10000,
			CallMS:       30000,
			ShutdownMS:   3000,
		},
		ToolAllowlist: []string{"read_file", "list"},
		RiskOverrides: map[string]string{"read_file": "low"},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"name", "command", "args", "env", "cwd", "enabled", "timeouts", "tool_allowlist", "risk_overrides",
	} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("serialized config is missing %q: %s", field, raw)
		}
	}
	var got MCPServerConfig
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMCPTimeoutsNormalizedUsesDefaultsForOmittedValues(t *testing.T) {
	want := MCPTimeouts{
		StartMS:      10000,
		InitializeMS: 10000,
		ListMS:       10000,
		CallMS:       30000,
		ShutdownMS:   3000,
	}
	if got := DefaultTimeouts(); got != want {
		t.Fatalf("default timeouts = %#v, want %#v", got, want)
	}
}

func TestMCPTimeoutsNormalizedPreservesProvidedValues(t *testing.T) {
	got := (MCPTimeouts{
		StartMS: -1,
		CallMS:  45000,
	}).Normalized()

	if got.StartMS != -1 {
		t.Fatalf("provided invalid value should remain available for validation, got %d", got.StartMS)
	}
	if got.CallMS != 45000 {
		t.Fatalf("provided call timeout = %d, want 45000", got.CallMS)
	}
	if got.InitializeMS != DefaultInitializeTimeoutMS ||
		got.ListMS != DefaultListTimeoutMS ||
		got.ShutdownMS != DefaultShutdownTimeoutMS {
		t.Fatalf("omitted values were not normalized: %#v", got)
	}
}

func TestMCPServerCRUDDTOJSONShape(t *testing.T) {
	response := MCPServerResponse{
		ID: "mcp_srv_1",
		MCPServerConfig: MCPServerConfig{
			Name:     "filesystem",
			Command:  "mcp-filesystem",
			Enabled:  true,
			Timeouts: DefaultTimeouts(),
		},
		CreatedAt: "2026-07-09T00:00:00Z",
		UpdatedAt: "2026-07-09T00:00:00Z",
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "name", "command", "enabled", "timeouts", "created_at", "updated_at"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("serialized response is missing %q: %s", field, raw)
		}
	}
	if _, nested := fields["MCPServerConfig"]; nested {
		t.Fatalf("embedded config must be flattened: %s", raw)
	}

	listRaw, err := json.Marshal(MCPServerListResponse{Servers: []MCPServerResponse{response}})
	if err != nil {
		t.Fatal(err)
	}
	var listFields map[string]json.RawMessage
	if err := json.Unmarshal(listRaw, &listFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := listFields["servers"]; !ok {
		t.Fatalf("serialized list response is missing servers: %s", listRaw)
	}
}

func TestMCPDiscoveryResultJSONRoundTrip(t *testing.T) {
	want := MCPDiscoveryResult{Servers: []MCPServerDiscovery{
		{
			Name:   "filesystem",
			Status: "ready",
			ServerInfo: MCPServerInfo{
				Name:    "filesystem-server",
				Version: "1.2.3",
			},
			Tools: []MCPToolDefinition{
				{
					Name:        "read_file",
					Description: "Read a workspace file.",
					InputSchema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"path": map[string]any{"type": "string"},
						},
					},
				},
			},
			StderrSummary: "diagnostic output",
			DurationMS:    42,
		},
		{
			Name:       "broken",
			Status:     "failed",
			Tools:      []MCPToolDefinition{},
			Error:      "initialize failed",
			DurationMS: 15,
		},
	}}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["servers"]; !ok {
		t.Fatalf("serialized discovery result is missing servers: %s", raw)
	}
	var serializedServers []map[string]json.RawMessage
	if err := json.Unmarshal(fields["servers"], &serializedServers); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "status", "server_info", "tools", "error", "stderr_summary", "duration_ms"} {
		if _, ok := serializedServers[0][field]; !ok {
			t.Fatalf("serialized server discovery is missing %q: %s", field, raw)
		}
	}
	var serializedTools []map[string]json.RawMessage
	if err := json.Unmarshal(serializedServers[0]["tools"], &serializedTools); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "description", "input_schema"} {
		if _, ok := serializedTools[0][field]; !ok {
			t.Fatalf("serialized tool definition is missing %q: %s", field, raw)
		}
	}
	var got MCPDiscoveryResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovery round trip mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}
