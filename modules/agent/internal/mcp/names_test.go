package mcp

import (
	"testing"

	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/tools"
)

func TestCanonicalNameAndRisk(t *testing.T) {
	if got := CanonicalName("filesystem", "read.file"); got != "mcp__filesystem__read_file" {
		t.Fatalf("canonical = %q", got)
	}
	cfg := protomcp.MCPServerConfig{
		Name:          "fake",
		RiskOverrides: map[string]string{"read_file": "low"},
	}
	if toolRisk(cfg, "read_file") != tools.RiskLow {
		t.Fatal("expected low risk override")
	}
	if toolRisk(cfg, "other") != tools.RiskHigh {
		t.Fatal("expected default high risk")
	}
}
