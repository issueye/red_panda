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

	// Index ephemeral inline payloads by attachment_id / path so history
	// image_ref blocks can rehydrate pixels when Gateway inlined them
	// (docs/51 strategy A + rehydrate recent images).
	inlineByRef := indexInlineAttachments(params.Input.Attachments)

	for _, message := range params.Session.Conversation {
		if !isModelConversationRole(message.Role) {
			continue
		}
		content := conversationMessageContent(message, inlineByRef)
		if content == nil || content == "" {
			continue
		}
		messages = append(messages, provider.Message{Role: message.Role, Content: content})
	}

	// Current user turn: text input + current-run attachments as multimodal parts.
	userContent := composeUserTurnContent(input, params.Input.Attachments)
	messages = append(messages, provider.Message{Role: "user", Content: userContent})

	reqAttachments := toRequestAttachments(params.Input.Attachments)

	return provider.Request{
		RunID:     params.RunID,
		SessionID: params.Session.ID,
		Input:     input,
		Messages:  messages,
		Options: provider.RequestOptions{
			ProviderName:      options.ProviderName,
			ProviderBaseURL:   options.ProviderBaseURL,
			ProviderAPIKey:    options.ProviderAPIKey,
			ProviderHTTPProxy: options.ProviderHTTPProxy,
			Stream:            options.ProviderStream,
			Model:             options.Model,
			EnableThinking:    options.EnableThinking,
			ReasoningEffort:   options.ReasoningEffort,
			LogLLMRequests:    options.LogLLMRequests,
		},
		Tools:       definitions,
		ToolHistory: history,
		ToolRounds:  rounds,
		Attachments: reqAttachments,
	}
}

// conversationMessageContent maps a stored conversation message into a
// provider.Message.Content value. Pure-text messages stay as string; messages
// with image_ref blocks become []Part, rehydrating pixels from inlineByRef
// when available, otherwise keeping an alt text placeholder so history images
// are never silently dropped (docs/52 Slice C — conversationMessageText fix).
func conversationMessageContent(message methods.Message, inlineByRef map[string]methods.InputAttachment) any {
	hasImage := false
	for _, block := range message.Content {
		if block.Type == "image_ref" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return conversationMessageText(message)
	}

	parts := make([]provider.Part, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case "", "text":
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, provider.Part{Type: "text", Text: text})
			}
		case "image_ref":
			parts = append(parts, imageRefPart(block, inlineByRef))
		default:
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, provider.Part{Type: "text", Text: text})
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return parts
}

func imageRefPart(block methods.ContentBlock, inlineByRef map[string]methods.InputAttachment) provider.Part {
	key := strings.TrimSpace(block.AttachmentID)
	if key == "" {
		key = strings.TrimSpace(block.Path)
	}
	if att, ok := inlineByRef[key]; ok && strings.TrimSpace(att.DataB64) != "" {
		mime := firstNonEmpty(att.MIME, block.MIME, "image/png")
		return provider.Part{
			Type:     "image_url",
			ImageURL: &provider.ImageURL{URL: provider.DataURL(mime, att.DataB64)},
		}
	}
	// No inline bytes available (older history or strategy-B path): keep a
	// textual placeholder so the model still knows an image was present.
	alt := strings.TrimSpace(block.Alt)
	if alt == "" {
		alt = key
	}
	if alt == "" {
		alt = "image"
	}
	label := fmt.Sprintf("[image: %s]", alt)
	if block.MIME != "" {
		label = fmt.Sprintf("[image: %s (%s)]", alt, block.MIME)
	}
	return provider.Part{Type: "text", Text: label}
}

// composeUserTurnContent builds the current user turn. With attachments that
// carry DataB64 it returns []Part; otherwise a plain string so pure-text paths
// keep the historical Content shape.
func composeUserTurnContent(input string, attachments []methods.InputAttachment) any {
	if len(attachments) == 0 {
		return input
	}
	parts := make([]provider.Part, 0, 1+len(attachments))
	if text := strings.TrimSpace(input); text != "" {
		parts = append(parts, provider.Part{Type: "text", Text: text})
	} else if input != "" {
		parts = append(parts, provider.Part{Type: "text", Text: input})
	}
	for _, att := range attachments {
		if strings.TrimSpace(att.DataB64) == "" {
			// Ref-only (no inline): surface a placeholder rather than dropping.
			label := firstNonEmpty(att.AttachmentID, att.Path, "image")
			parts = append(parts, provider.Part{
				Type: "text",
				Text: fmt.Sprintf("[image: %s]", label),
			})
			continue
		}
		mime := firstNonEmpty(att.MIME, "image/png")
		parts = append(parts, provider.Part{
			Type:     "image_url",
			ImageURL: &provider.ImageURL{URL: provider.DataURL(mime, att.DataB64)},
		})
	}
	if len(parts) == 0 {
		return input
	}
	return parts
}

func indexInlineAttachments(attachments []methods.InputAttachment) map[string]methods.InputAttachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make(map[string]methods.InputAttachment, len(attachments)*2)
	for _, att := range attachments {
		if id := strings.TrimSpace(att.AttachmentID); id != "" {
			out[id] = att
		}
		if path := strings.TrimSpace(att.Path); path != "" {
			out[path] = att
		}
	}
	return out
}

func toRequestAttachments(attachments []methods.InputAttachment) []provider.RequestAttachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]provider.RequestAttachment, 0, len(attachments))
	for _, att := range attachments {
		out = append(out, provider.RequestAttachment{
			ID:       att.AttachmentID,
			Path:     att.Path,
			MIME:     att.MIME,
			DataB64:  att.DataB64,
			ByteSize: att.ByteSize,
		})
	}
	return out
}

func conversationMessageText(message methods.Message) string {
	parts := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case "image_ref":
			// Text-only view: keep an alt placeholder so callers that still
			// join text do not silently erase the image's existence.
			alt := strings.TrimSpace(block.Alt)
			if alt == "" {
				alt = firstNonEmpty(block.AttachmentID, block.Path, "image")
			}
			parts = append(parts, fmt.Sprintf("[image: %s]", alt))
		default:
			if text := strings.TrimSpace(block.Text); text != "" {
				parts = append(parts, text)
			}
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
