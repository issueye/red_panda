package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func openAICompatibleMessages(req ProviderRequest) []map[string]any {
	messages := make([]map[string]any, 0, 4+len(req.Session.Conversation)+1+len(req.ToolHistory)*2)
	// 始终注入最新本地时间，避免模型依赖训练数据中的日期。
	messages = append(messages, map[string]any{
		"role":    "system",
		"content": currentTimeContextMessage(time.Now()),
	})
	goalController := hasToolNamed(req.Tools, "goal.create")
	if hasToolNamed(req.Tools, "worker.delegate") && !goalController {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentOrchestrationPolicy,
		})
	}
	if hasSuccessfulWorkerResult(req.ToolHistory) && !goalController {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentPostDelegationPolicy,
		})
	}
	if goalController {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentGoalControllerPolicy,
		})
	}
	if hasToolNamed(req.Tools, "todo.write") && !goalController {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": rootAgentTodoPolicy,
		})
	}
	// 专业角色、工作进程和技能的角色简介与长期记忆分离。
	if req.Options.SpecialistContext != nil && strings.TrimSpace(req.Options.SpecialistContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.SpecialistContext.Context),
		})
	}
	if req.Options.MemoryContext != nil && strings.TrimSpace(req.Options.MemoryContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.MemoryContext.Context),
		})
	}
	if req.Options.GoalContext != nil && strings.TrimSpace(req.Options.GoalContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.GoalContext.Context),
		})
		// 用户或命令可能已创建并绑定 Goal，此时继续现有控制器状态。
		if id := strings.TrimSpace(req.Options.GoalContext.GoalID); id != "" {
			messages = append(messages, map[string]any{
				"role": "system",
				"content": "A Goal is already bound to this run (id=" + id + "). " +
					"Do NOT create another. Continue from its criteria, evidence, action queue, and last assessment. " +
					"Use goal.plan / goal.observe / goal.assess / goal.finish as the feedback loop requires.",
			})
		}
	}
	if req.Options.TodoContext != nil && strings.TrimSpace(req.Options.TodoContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.TodoContext.Context),
		})
	}
	if req.Options.SkillsContext != nil && strings.TrimSpace(req.Options.SkillsContext.Context) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.TrimSpace(req.Options.SkillsContext.Context),
		})
	}
	for _, message := range req.Session.Conversation {
		content := ConversationMessageText(message)
		if content == "" {
			continue
		}
		role, ok := openAICompatibleConversationRole(message.Role)
		if !ok {
			continue
		}
		messages = append(messages, map[string]any{
			"role":    role,
			"content": content,
		})
	}
	messages = append(messages, map[string]any{"role": "user", "content": req.Input.Text})
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

func hasSuccessfulWorkerResult(history []ToolExchange) bool {
	for _, exchange := range history {
		if exchange.Call.Name == "worker.delegate" && exchange.Result.Status == tools.CallStatusCompleted {
			return true
		}
	}
	return false
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

// currentTimeContextMessage 构建权威的本地时间系统提示。
// 每个提供方回合均会注入，确保多步骤工具循环也保持准确。
func currentTimeContextMessage(now time.Time) string {
	zoneName, offsetSec := now.Zone()
	if zoneName == "" {
		zoneName = "Local"
	}
	weekdayCN := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	weekday := weekdayCN[int(now.Weekday())]
	return fmt.Sprintf(
		"当前权威时间（本地时区）：%s %s（%s，UTC%s）。\n"+
			"Current local time: %s (%s, UTC%s).\n"+
			"请以此时间为准回答日期/时间相关问题，不要使用训练数据中的过时日期。",
		now.Format("2006-01-02 15:04:05"),
		weekday,
		zoneName,
		formatUTCOffset(offsetSec),
		now.Format(time.RFC3339),
		zoneName,
		formatUTCOffset(offsetSec),
	)
}

func formatUTCOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	mins := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, mins)
}

func hasToolNamed(definitions []tools.Definition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

// maxToolResultForModel 限制每条回传给下一轮 LLM 的工具结果。
// 完整标准输出仍保留在工具事件和 UI 工具卡片中。
const maxToolResultForModel = 16 * 1024

// toolExchangeContent 为下一轮提供方调用格式化工具结果。
// 始终返回标准 JSON 封装，绝不静默丢弃字段。
func toolExchangeContent(result tools.Result) string {
	return agenttools.ModelFacingToolContent(result)
}

func openAICompatibleConversationRole(role string) (string, bool) {
	if role == "user" || role == "system" {
		return role, true
	}
	if role == "assistant" {
		return role, true
	}
	return "", false
}

func ConversationMessageText(message methods.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
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
