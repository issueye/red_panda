package runtime

// Type aliases keep call sites readable after the B4b package split (docs/36).
// Leaf packages own the implementations; runtime remains the composition root.

import (
	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type (
	Provider        = provider.Provider
	ProviderRequest = provider.ProviderRequest
	ProviderChunk   = provider.ProviderChunk
	ToolExchange    = provider.ToolExchange
	ToolRunner      = agenttools.ToolRunner
	ToolRunContext  = agenttools.ToolRunContext
	ToolInvocation  = agenttools.ToolInvocation
	SubagentManager = agenttools.SubagentManager
	ProcessSubAgent = subagent.Process
	ProcessPool     = subagent.ProcessPool
	MCPManager      = agentmcp.Manager
	MCPToolBinding  = agentmcp.ToolBinding
)

// Policy helpers (owned by tools package).
type ToolDecision = agenttools.ToolDecision

const (
	ToolDecisionAllow             = agenttools.ToolDecisionAllow
	ToolDecisionDeny              = agenttools.ToolDecisionDeny
	ToolDecisionRequirePermission = agenttools.ToolDecisionRequirePermission
)

func EvaluateToolPolicy(options methods.ReplyOptions, call tools.Call) ToolDecision {
	return agenttools.EvaluateToolPolicy(options, call)
}

func availableToolsForOptions(definitions []tools.Definition, options methods.ReplyOptions) []tools.Definition {
	return agenttools.AvailableToolsForOptions(definitions, options)
}
