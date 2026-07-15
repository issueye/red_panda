package provider

import (
	"encoding/json"
	"strings"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/tools"
)

func openAICompatibleMessages(req ProviderRequest) []map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+len(req.ToolHistory)*2)
	for _, message := range req.Messages {
		messages = append(messages, map[string]any{
			"role":    message.Role,
			"content": message.Content,
		})
	}
	for _, round := range toolRoundsForRequest(req) {
		if len(round) == 0 {
			continue
		}
		toolCalls := make([]map[string]any, 0, len(round))
		for _, exchange := range round {
			arguments, _ := json.Marshal(exchange.Call.Arguments)
			toolCalls = append(toolCalls, map[string]any{
				"id":   exchange.Call.ID,
				"type": "function",
				"function": map[string]any{
					"name":      publicToolName(exchange.Call.Name),
					"arguments": string(arguments),
				},
			})
		}
		messages = append(messages, map[string]any{
			"role":       "assistant",
			"tool_calls": toolCalls,
		})
		for _, exchange := range round {
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": exchange.Call.ID,
				"name":         publicToolName(exchange.Call.Name),
				"content":      toolExchangeContent(exchange.Result),
			})
		}
	}
	return messages
}

func toolRoundsForRequest(req ProviderRequest) [][]ToolExchange {
	if len(req.ToolRounds) > 0 {
		return req.ToolRounds
	}
	if len(req.ToolHistory) == 0 {
		return nil
	}
	rounds := make([][]ToolExchange, 0, len(req.ToolHistory))
	for _, exchange := range req.ToolHistory {
		rounds = append(rounds, []ToolExchange{exchange})
	}
	return rounds
}

// maxToolResultForModel 限制每条回传给下一轮 LLM 的工具结果。
// 完整标准输出仍保留在工具事件和 UI 工具卡片中。
const maxToolResultForModel = 16 * 1024

// toolExchangeContent 为下一轮提供方调用格式化工具结果。
// 始终返回标准 JSON 封装，绝不静默丢弃字段。
func toolExchangeContent(result tools.Result) string {
	return agenttools.ModelFacingToolContent(result)
}

func openAICompatibleTools(definitions []tools.Definition) []map[string]any {
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        publicToolName(definition.Name),
				"description": definition.Description,
				"parameters":  definition.Parameters,
			},
		})
	}
	return items
}

func publicToolName(name string) string {
	return strings.ReplaceAll(name, ".", "__")
}

func internalToolName(name string) string {
	return strings.ReplaceAll(name, "__", ".")
}
