package tools

import (
	"fmt"
	"os"
	"strings"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	ptools "redpanda/protocol/tools"
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

func EvaluateToolPolicy(options methods.ReplyOptions, call ptools.Call, opsOnly bool) ToolDecision {
	if ContainsString(options.ToolDenylist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is denied by tool_denylist"}
	}
	allowlist := effectiveToolAllowlist(options)
	if len(allowlist) > 0 && !ContainsString(allowlist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is not in tool_allowlist"}
	}
	if opsOnly && !debugToolsEnabled(options) && !ContainsString(allowlist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "ops tool is not enabled (set debug_tools or RED_PANDA_DEBUG_TOOLS)"}
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

func debugToolsEnabled(options methods.ReplyOptions) bool {
	if options.DebugTools {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_DEBUG_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func riskBasedDecision(permissionMode string, risk ptools.Risk) ToolDecision {
	if risk == ptools.RiskLow {
		return ToolDecision{Action: ToolDecisionAllow, Reason: "low risk tool"}
	}
	switch permission.Mode(permissionMode) {
	case permission.ModeAllowAll:
		return ToolDecision{Action: ToolDecisionAllow, Reason: "permission_mode allow_all"}
	case permission.ModeDenyAll:
		return ToolDecision{Action: ToolDecisionDeny, Reason: "permission_mode deny_all"}
	case permission.ModePermissive:
		if risk == ptools.RiskHigh {
			return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "high risk tool in permissive mode"}
		}
		return ToolDecision{Action: ToolDecisionAllow, Reason: "non-high risk tool in permissive mode"}
	default:
		return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "strict permission mode"}
	}
}

func ContainsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// effectiveToolAllowlist returns the allowlist the tool schema and policy use.
// With the Goal feature removed there is no goal-mode tightening; this is now
// a pass-through of the client-supplied ToolAllowlist.
func effectiveToolAllowlist(options methods.ReplyOptions) []string {
	return options.ToolAllowlist
}
