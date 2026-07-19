package provider

import (
	"context"

	"redpanda/protocol/tools"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error
}

type Message struct {
	Role    string
	Content string
}

type RequestOptions struct {
	ProviderName    string
	ProviderBaseURL string
	ProviderAPIKey  string
	Stream          *bool
	Model           string
	ReasoningEffort string
	LogLLMRequests  bool
}

type Request struct {
	RunID     string
	SessionID string
	Input     string
	Messages  []Message
	Options   RequestOptions
	Tools     []tools.Definition
	// ToolHistory 是平铺列表，供 EchoProvider 和测试使用。
	ToolHistory []ToolExchange
	// ToolRounds 对同一模型回合运行的工具分组，符合 OpenAI 多工具格式。
	// 为空时，每个 ToolHistory 项视为独立回合。
	ToolRounds [][]ToolExchange
}

// ProviderRequest is kept as a source-compatible alias for provider fakes.
// New production code should use Request.
type ProviderRequest = Request

type ToolExchange struct {
	Call   tools.Call
	Result tools.Result
}

type ProviderChunk struct {
	Delta     string
	Final     bool
	ToolCalls []tools.Call
}
