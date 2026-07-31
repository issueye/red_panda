package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"redpanda/protocol/tools"
)

const defaultAnthropicMaxTokens = 4096

type AnthropicProvider struct{ providerConfig }

func (AnthropicProvider) Name() string { return "anthropic" }

func (p AnthropicProvider) Complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	if override, ok := Resolve(req.Options, p.Stream); ok {
		return completeResolvedProvider(ctx, override, req, emit)
	}
	return p.complete(ctx, req, emit)
}

func (p AnthropicProvider) complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	return completeWithRetry(ctx, p.providerConfig, req, emit, p.completeAttempt)
}

func (p AnthropicProvider) completeAttempt(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	model := req.Options.Model
	if model == "" {
		model = p.Model
	}
	system, messages := anthropicMessages(req)
	body := map[string]any{"model": model, "max_tokens": defaultAnthropicMaxTokens, "messages": messages, "stream": p.Stream}
	if req.Options.EnableThinking {
		body["thinking"] = map[string]any{"type": "adaptive"}
		if effort := strings.TrimSpace(req.Options.ReasoningEffort); effort != "" {
			if effort == "xhigh" {
				effort = "max"
			}
			body["output_config"] = map[string]any{"effort": effort}
		}
	}
	if len(system) > 0 {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		body["tools"] = anthropicTools(req.Tools, explicitCacheEnabled(req.Options, "anthropic"))
		body["tool_choice"] = map[string]any{"type": "auto"}
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := anthropicMessagesURL(p.BaseURL)
	logProviderRequest(req, model, endpoint, rawBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.APIKey)
	}
	applySafeHeaders(httpReq, req.Options.Headers)
	resp, err := p.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return providerHTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if p.Stream {
		return completeAnthropicStream(resp.Body, emit)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return completeAnthropicResponse(raw, emit)
}

func anthropicMessagesURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(base, "/messages") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

func anthropicMessages(req Request) ([]map[string]any, []map[string]any) {
	var systems []map[string]any
	promptMessages := req.Prompt.FlattenMessages()
	messages := make([]map[string]any, 0, len(promptMessages)+len(req.ToolHistory)*2)
	cacheableSystemCount := 0
	explicitCache := explicitCacheEnabled(req.Options, "anthropic")
	for index, message := range promptMessages {
		if message.Role == "system" {
			// System prompts stay plain text (Anthropic top-level "system" field).
			if text := strings.TrimSpace(MessageText(message.Content)); text != "" {
				block := map[string]any{"type": "text", "text": text}
				systems = append(systems, block)
				if explicitCache && index < len(req.Prompt.StablePrefix)+len(req.Prompt.SessionPrefix) {
					cacheableSystemCount = len(systems)
				}
			}
			continue
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": anthropicMessageContent(message.Content)})
	}
	for _, round := range toolRoundsForRequest(req) {
		contents := toolRoundModelContents(round)
		uses := make([]map[string]any, 0, len(round))
		results := make([]map[string]any, 0, len(round))
		for index, exchange := range round {
			uses = append(uses, map[string]any{"type": "tool_use", "id": exchange.Call.ID, "name": publicToolName(exchange.Call.Name), "input": exchange.Call.Arguments})
			results = append(results, map[string]any{"type": "tool_result", "tool_use_id": exchange.Call.ID, "content": contents[index], "is_error": exchange.Result.Status != tools.CallStatusCompleted})
		}
		if len(uses) > 0 {
			messages = append(messages, map[string]any{"role": "assistant", "content": uses}, map[string]any{"role": "user", "content": results})
		}
	}
	if cacheableSystemCount > 0 {
		systems[cacheableSystemCount-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	}
	return systems, messages
}

func anthropicTools(definitions []tools.Definition, explicitCache bool) []map[string]any {
	definitions = CanonicalToolDefinitions(definitions)
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{"name": publicToolName(definition.Name), "description": definition.Description, "input_schema": definition.Parameters})
	}
	if explicitCache && len(items) > 0 {
		items[len(items)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	}
	return items
}

type anthropicContent struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`
}

func completeAnthropicResponse(raw []byte, emit func(ProviderChunk) error) error {
	var response struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			CacheRead    int `json:"cache_read_input_tokens"`
			CacheWrite   int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		Content []anthropicContent `json:"content"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("provider error: %s", response.Error.Message)
	}
	usage := anthropicProviderUsage(response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.CacheRead, response.Usage.CacheWrite)
	var calls []tools.Call
	var text strings.Builder
	var reasoning strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
		if block.Type == "tool_use" {
			calls = append(calls, tools.Call{ID: block.ID, Name: internalToolName(block.Name), Arguments: block.Input})
		}
		if block.Type == "thinking" {
			reasoning.WriteString(block.Thinking)
		}
	}
	if reasoning.Len() > 0 {
		if err := emit(ProviderChunk{ReasoningDelta: reasoning.String()}); err != nil {
			return err
		}
	}
	if len(calls) > 0 {
		return emit(ProviderChunk{ToolCalls: calls, Usage: usage})
	}
	if text.Len() > 0 {
		if err := emit(ProviderChunk{Delta: text.String(), Usage: usage}); err != nil {
			return err
		}
	}
	return emit(ProviderChunk{Final: true})
}

func anthropicProviderUsage(inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int) *ProviderUsage {
	return providerUsageOrNil(ProviderUsage{
		InputTokens:      inputTokens + cacheReadTokens + cacheWriteTokens,
		OutputTokens:     outputTokens,
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
	})
}

type anthropicStreamCall struct {
	index    int
	id, name string
	input    strings.Builder
}

func completeAnthropicStream(reader io.Reader, emit func(ProviderChunk) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	calls := map[int]*anthropicStreamCall{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var event struct {
			Type         string           `json:"type"`
			Index        int              `json:"index"`
			ContentBlock anthropicContent `json:"content_block"`
			Delta        struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Message *struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
					CacheRead   int `json:"cache_read_input_tokens"`
					CacheWrite  int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
				CacheRead    int `json:"cache_read_input_tokens"`
				CacheWrite   int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return err
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil && (event.Message.Usage.InputTokens > 0 || event.Message.Usage.CacheRead > 0 || event.Message.Usage.CacheWrite > 0) {
				if err := emit(ProviderChunk{Usage: anthropicProviderUsage(event.Message.Usage.InputTokens, 0, event.Message.Usage.CacheRead, event.Message.Usage.CacheWrite)}); err != nil {
					return err
				}
			}
		case "message_delta":
			if event.Usage.OutputTokens > 0 || event.Usage.CacheRead > 0 || event.Usage.CacheWrite > 0 {
				if err := emit(ProviderChunk{Usage: anthropicProviderUsage(0, event.Usage.OutputTokens, event.Usage.CacheRead, event.Usage.CacheWrite)}); err != nil {
					return err
				}
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				calls[event.Index] = &anthropicStreamCall{index: event.Index, id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			if event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "" {
				if err := emit(ProviderChunk{ReasoningDelta: event.Delta.Thinking}); err != nil {
					return err
				}
			}
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				if err := emit(ProviderChunk{Delta: event.Delta.Text}); err != nil {
					return err
				}
			}
			if event.Delta.Type == "input_json_delta" {
				call := calls[event.Index]
				if call == nil {
					call = &anthropicStreamCall{index: event.Index}
					calls[event.Index] = call
				}
				call.input.WriteString(event.Delta.PartialJSON)
			}
		case "error":
			message := "unknown error"
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
			}
			return fmt.Errorf("provider error: %s", message)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(calls) > 0 {
		indexes := make([]int, 0, len(calls))
		for index := range calls {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		items := make([]tools.Call, 0, len(indexes))
		for _, index := range indexes {
			call := calls[index]
			args := map[string]any{}
			if call.input.Len() > 0 {
				_ = json.Unmarshal([]byte(call.input.String()), &args)
			}
			items = append(items, tools.Call{ID: call.id, Name: internalToolName(call.name), Arguments: args})
		}
		return emit(ProviderChunk{ToolCalls: items})
	}
	return emit(ProviderChunk{Final: true})
}
