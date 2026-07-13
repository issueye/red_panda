package runtime

// Test aliases keep runtime integration tests concise while production code
// depends directly on the packages that own these types.

import (
	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type (
	ProviderRequest = provider.ProviderRequest
	ProviderChunk   = provider.ProviderChunk
	ToolExchange    = provider.ToolExchange
	ToolRunner      = agenttools.ToolRunner
	ToolRunContext  = agenttools.ToolRunContext
	ToolInvocation  = agenttools.ToolInvocation
	ProcessSubAgent = subagent.Process
)

func availableToolsForOptions(definitions []tools.Definition, options methods.ReplyOptions) []tools.Definition {
	return agenttools.AvailableToolsForOptions(definitions, options)
}
