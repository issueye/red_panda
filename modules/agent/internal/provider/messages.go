package provider

import (
	"encoding/json"
	"strings"

	"redpanda/protocol/tools"
)

const (
	maxToolRoundModelBytes     = 8 * 1024
	maxWorkerReportRoundBytes  = 256 * 1024
	maxWorkerReportResultBytes = 96 * 1024
)

func openAICompatibleMessages(req ProviderRequest) []map[string]any {
	promptMessages := req.Prompt.FlattenMessages()
	messages := make([]map[string]any, 0, len(promptMessages)+len(req.ToolHistory)*2)
	for _, message := range promptMessages {
		messages = append(messages, map[string]any{
			"role":    message.Role,
			"content": openAICompatibleContent(message.Content),
		})
	}
	for _, round := range toolRoundsForRequest(req) {
		if len(round) == 0 {
			continue
		}
		contents := toolRoundModelContents(round)
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
		for index, exchange := range round {
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": exchange.Call.ID,
				"name":         publicToolName(exchange.Call.Name),
				"content":      contents[index],
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

// toolExchangeContent 为下一轮提供方调用格式化工具结果。
// 始终返回标准 JSON 封装，绝不静默丢弃字段。
// 格式化归属 protocol/tools，避免 provider 依赖 agent/internal/tools（docs/39 Wave 2）。
func toolExchangeContent(result tools.Result) string {
	return tools.ModelFacingContent(result)
}

func toolRoundModelContents(round []ToolExchange) []string {
	if len(round) == 0 {
		return nil
	}
	ordinaryCount := 0
	workerReportCount := 0
	for _, exchange := range round {
		if isWorkerReportTool(exchange.Call.Name) {
			workerReportCount++
		} else {
			ordinaryCount++
		}
	}
	contents := make([]string, len(round))
	for index, exchange := range round {
		limit := sharedToolResultBudget(maxToolRoundModelBytes, ordinaryCount, 512)
		result := exchange.Result
		if isWorkerReportTool(exchange.Call.Name) {
			limit = sharedToolResultBudget(maxWorkerReportRoundBytes, workerReportCount, 4096)
			if limit > maxWorkerReportResultBytes {
				limit = maxWorkerReportResultBytes
			}
			result = workerReportModelResult(result)
		}
		contents[index] = tools.ModelFacingContentWithLimit(result, limit)
	}
	return contents
}

func sharedToolResultBudget(total int, count int, minimum int) int {
	if count <= 0 {
		return minimum
	}
	budget := total / count
	if budget < minimum {
		return minimum
	}
	return budget
}

func isWorkerReportTool(name string) bool {
	return name == "worker.delegate" || name == "worker.result"
}

func workerReportModelResult(result tools.Result) tools.Result {
	var payload map[string]any
	if json.Unmarshal([]byte(result.Output), &payload) != nil {
		return result
	}
	report, _ := payload["output"].(string)
	if strings.TrimSpace(report) == "" {
		return result
	}
	delete(payload, "output")
	metadata, _ := json.Marshal(payload)
	next := result
	next.Output = "Worker report metadata: " + string(metadata) + "\n\n" + report
	return next
}

func openAICompatibleTools(definitions []tools.Definition) []map[string]any {
	definitions = CanonicalToolDefinitions(definitions)
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
