package runtime

// 测试别名让 Runtime 集成测试保持简洁；生产代码仍直接依赖类型所属的包。

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
