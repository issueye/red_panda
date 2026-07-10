package runtime

import (
	"fmt"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

type ToolDecisionAction string

const (
	ToolDecisionAllow             ToolDecisionAction = "allow"
	ToolDecisionRequirePermission ToolDecisionAction = "require_permission"
	ToolDecisionDeny              ToolDecisionAction = "deny"
)

type ToolDecision struct {
	Action ToolDecisionAction
	Reason string
}

func EvaluateToolPolicy(options methods.ReplyOptions, call tools.Call) ToolDecision {
	if containsString(options.ToolDenylist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is denied by tool_denylist"}
	}
	if len(options.ToolAllowlist) > 0 && !containsString(options.ToolAllowlist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is not in tool_allowlist"}
	}

	switch options.ToolPolicy {
	case "deny_all":
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool_policy denied all tools"}
	case "allow_all":
		return ToolDecision{Action: ToolDecisionAllow, Reason: "tool_policy allowed all tools"}
	case "ask_all":
		return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "tool_policy requires permission for all tools"}
	case "risk_based", "":
		return riskBasedDecision(options.PermissionMode, call.Risk)
	default:
		return ToolDecision{
			Action: ToolDecisionDeny,
			Reason: fmt.Sprintf("unknown tool_policy %q", options.ToolPolicy),
		}
	}
}

func riskBasedDecision(permissionMode string, risk tools.Risk) ToolDecision {
	if risk == tools.RiskLow {
		return ToolDecision{Action: ToolDecisionAllow, Reason: "low risk tool"}
	}
	switch permission.Mode(permissionMode) {
	case permission.ModeAllowAll:
		return ToolDecision{Action: ToolDecisionAllow, Reason: "permission_mode allow_all"}
	case permission.ModeDenyAll:
		return ToolDecision{Action: ToolDecisionDeny, Reason: "permission_mode deny_all"}
	case permission.ModePermissive:
		if risk == tools.RiskHigh {
			return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "high risk tool in permissive mode"}
		}
		return ToolDecision{Action: ToolDecisionAllow, Reason: "non-high risk tool in permissive mode"}
	default:
		return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "strict permission mode"}
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func availableToolsForOptions(definitions []tools.Definition, options methods.ReplyOptions) []tools.Definition {
	filtered := make([]tools.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if containsString(options.ToolDenylist, definition.Name) {
			continue
		}
		if len(options.ToolAllowlist) > 0 && !containsString(options.ToolAllowlist, definition.Name) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}
