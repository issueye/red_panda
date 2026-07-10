package runtime

import (
	"testing"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestEvaluateToolPolicy(t *testing.T) {
	highRiskShell := tools.Call{Name: "shell.exec", Risk: tools.RiskHigh}
	lowRiskRead := tools.Call{Name: "workspace.read_file", Risk: tools.RiskLow}

	tests := []struct {
		name     string
		options  methods.ReplyOptions
		call     tools.Call
		expected ToolDecisionAction
	}{
		{
			name: "denylist wins",
			options: methods.ReplyOptions{
				ToolDenylist: []string{"shell.exec"},
				ToolPolicy:   "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionDeny,
		},
		{
			name: "allowlist blocks missing tool",
			options: methods.ReplyOptions{
				ToolAllowlist: []string{"workspace.read_file"},
				ToolPolicy:    "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionDeny,
		},
		{
			name: "allow all permits high risk",
			options: methods.ReplyOptions{
				ToolPolicy: "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionAllow,
		},
		{
			name: "ask all prompts low risk",
			options: methods.ReplyOptions{
				ToolPolicy: "ask_all",
			},
			call:     lowRiskRead,
			expected: ToolDecisionRequirePermission,
		},
		{
			name: "strict risk based prompts high risk",
			options: methods.ReplyOptions{
				PermissionMode: "strict",
				ToolPolicy:     "risk_based",
			},
			call:     highRiskShell,
			expected: ToolDecisionRequirePermission,
		},
		{
			name: "risk based allows low risk",
			options: methods.ReplyOptions{
				PermissionMode: "strict",
			},
			call:     lowRiskRead,
			expected: ToolDecisionAllow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := EvaluateToolPolicy(test.options, test.call)
			if decision.Action != test.expected {
				t.Fatalf("expected %s, got %s (%s)", test.expected, decision.Action, decision.Reason)
			}
		})
	}
}

func TestAvailableToolsForOptionsHidesDeniedAndUnlistedTools(t *testing.T) {
	definitions := []tools.Definition{
		{Name: "workspace.read_file"},
		{Name: "skill.run"},
		{Name: "shell.exec"},
	}
	filtered := availableToolsForOptions(definitions, methods.ReplyOptions{
		ToolAllowlist: []string{"workspace.read_file", "skill.run"},
		ToolDenylist:  []string{"skill.run"},
	})
	if len(filtered) != 1 || filtered[0].Name != "workspace.read_file" {
		t.Fatalf("filtered tools = %#v, want workspace.read_file only", filtered)
	}
}
