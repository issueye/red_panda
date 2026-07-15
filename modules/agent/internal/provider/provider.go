package provider

import (
	"context"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req ProviderRequest, emit func(ProviderChunk) error) error
}

type ProviderRequest struct {
	RunID   string
	Session methods.ReplySession
	Input   methods.ReplyInput
	Options methods.ReplyOptions
	Tools   []tools.Definition
	// ToolHistory 是平铺列表，供 EchoProvider 和测试使用。
	ToolHistory []ToolExchange
	// ToolRounds 对同一模型回合运行的工具分组，符合 OpenAI 多工具格式。
	// 为空时，每个 ToolHistory 项视为独立回合。
	ToolRounds [][]ToolExchange
}

type ToolExchange struct {
	Call   tools.Call
	Result tools.Result
}

type ProviderChunk struct {
	Delta     string
	Final     bool
	ToolCalls []tools.Call
}
