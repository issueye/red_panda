package tools

import (
	"fmt"
	"os"
	"strings"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	ptools "redpanda/protocol/tools"
)

// opsOnlyTools 会注册给桌面端、命令行和调试用途，但默认不暴露给提供方工具架构（检查项 O1/O3；docs/41 W1-2）。
// 可通过 RED_PANDA_DEBUG_TOOLS=1、ReplyOptions.DebugTools 或显式 tool_allowlist 项启用。
var opsOnlyTools = map[string]struct{}{
	"skill.create":       {},
	"skill.update":       {},
	"skill.delete":       {},
	"worker.pool_status": {},
	"worker.pool_resize": {},
	"worker.pool_reset":  {},
	// Worker mailbox messaging is advanced orchestration; default Goal/chat paths
	// use worker.delegate + shared context.* instead (docs/41 W1-2).
	"worker.send":    {},
	"worker.receive": {},
}

func isOpsOnlyTool(name string) bool {
	_, ok := opsOnlyTools[strings.TrimSpace(name)]
	return ok
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

func opsToolExposed(options methods.ReplyOptions, name string) bool {
	if !isOpsOnlyTool(name) {
		return true
	}
	if debugToolsEnabled(options) {
		return true
	}
	// 显式允许单个运维工具，无需开放全部调试工具。
	return ContainsString(options.ToolAllowlist, name)
}

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

func EvaluateToolPolicy(options methods.ReplyOptions, call ptools.Call) ToolDecision {
	if ContainsString(options.ToolDenylist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is denied by tool_denylist"}
	}
	allowlist := effectiveToolAllowlist(options)
	if len(allowlist) > 0 && !ContainsString(allowlist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is not in tool_allowlist"}
	}
	// opsToolExposed 可将客户端显式允许列表视为运维工具的启用信号。
	eff := options
	eff.ToolAllowlist = allowlist
	if !opsToolExposed(eff, call.Name) {
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

func AvailableToolsForOptions(definitions []ptools.Definition, options methods.ReplyOptions) []ptools.Definition {
	allowlist := effectiveToolAllowlist(options)
	// 复制选项，让 opsToolExposed 和拒绝列表检查仍能看到原始值，
	// 同时允许列表校验使用实际生效的列表。
	eff := options
	eff.ToolAllowlist = allowlist

	filtered := make([]ptools.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if ContainsString(options.ToolDenylist, definition.Name) {
			continue
		}
		if len(allowlist) > 0 && !ContainsString(allowlist, definition.Name) {
			continue
		}
		if !opsToolExposed(eff, definition.Name) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}
