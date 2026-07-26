package runtime

import (
	"fmt"
	"strings"
	"time"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type promptComposer struct {
	now func() time.Time
}

func newPromptComposer() promptComposer {
	return promptComposer{now: time.Now}
}

func (c promptComposer) compose(params methods.ReplyParams, input string, definitions []tools.Definition, rounds [][]provider.ToolExchange) provider.Request {
	history := flattenToolRounds(rounds)
	messages := make([]provider.Message, 0, 8+len(params.Session.Conversation))
	appendSystem := func(content string) {
		if content = strings.TrimSpace(content); content != "" {
			messages = append(messages, provider.Message{Role: "system", Content: content})
		}
	}
	appendSystem(currentTimeContextMessage(c.now()))

	if hasToolNamed(definitions, "worker.delegate") {
		appendSystem(rootAgentOrchestrationPolicy)
	}
	if hasSuccessfulWorkerResult(history) {
		appendSystem(rootAgentPostDelegationPolicy)
	}
	if hasToolNamed(definitions, "todo.write") {
		appendSystem(rootAgentTodoPolicy)
	}
	if options := params.Options; options.SpecialistContext == nil && hasToolNamed(definitions, "git.status") {
		appendSystem(rootAgentFileChangeReportPolicy)
	}

	options := params.Options
	if options.SpecialistContext != nil {
		appendSystem(options.SpecialistContext.Context)
	}
	if options.MemoryContext != nil {
		appendSystem(options.MemoryContext.Context)
	}
	if options.TodoContext != nil {
		appendSystem(options.TodoContext.Context)
	}
	if options.SkillsContext != nil {
		appendSystem(options.SkillsContext.Context)
	}

	for _, message := range params.Session.Conversation {
		content := conversationMessageText(message)
		if content == "" || !isModelConversationRole(message.Role) {
			continue
		}
		messages = append(messages, provider.Message{Role: message.Role, Content: content})
	}
	messages = append(messages, provider.Message{Role: "user", Content: input})

	return provider.Request{
		RunID:     params.RunID,
		SessionID: params.Session.ID,
		Input:     input,
		Messages:  messages,
		Options: provider.RequestOptions{
			ProviderName:    options.ProviderName,
			ProviderBaseURL: options.ProviderBaseURL,
			ProviderAPIKey:  options.ProviderAPIKey,
			Stream:          options.ProviderStream,
			Model:           options.Model,
			ReasoningEffort: options.ReasoningEffort,
			LogLLMRequests:  options.LogLLMRequests,
		},
		Tools:       definitions,
		ToolHistory: history,
		ToolRounds:  rounds,
	}
}

func conversationMessageText(message methods.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		if text := strings.TrimSpace(block.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func isModelConversationRole(role string) bool {
	return role == "user" || role == "system" || role == "assistant"
}

func hasToolNamed(definitions []tools.Definition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func hasSuccessfulWorkerResult(history []provider.ToolExchange) bool {
	for _, exchange := range history {
		if exchange.Call.Name == "worker.delegate" && exchange.Result.Status == tools.CallStatusCompleted {
			return true
		}
	}
	return false
}

func currentTimeContextMessage(now time.Time) string {
	zoneName, offsetSec := now.Zone()
	if zoneName == "" {
		zoneName = "Local"
	}
	weekdayCN := [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	return fmt.Sprintf(
		"当前权威时间（本地时区）：%s %s（%s，UTC%s）。\n"+
			"Current local time: %s (%s, UTC%s).\n"+
			"请以此时间为准回答日期/时间相关问题，不要使用训练数据中的过时日期。",
		now.Format("2006-01-02 15:04:05"), weekdayCN[int(now.Weekday())], zoneName, formatUTCOffset(offsetSec),
		now.Format(time.RFC3339), zoneName, formatUTCOffset(offsetSec),
	)
}

func formatUTCOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	return fmt.Sprintf("%s%02d:%02d", sign, offsetSec/3600, (offsetSec%3600)/60)
}
