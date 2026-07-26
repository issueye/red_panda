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
	if effort := strings.TrimSpace(req.Options.ReasoningEffort); effort != "" {
		if effort == "xhigh" {
			effort = "max"
		}
		body["output_config"] = map[string]any{"effort": effort}
	}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		body["tools"] = anthropicTools(req.Tools)
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

func anthropicMessages(req Request) (string, []map[string]any) {
	var systems []string
	messages := make([]map[string]any, 0, len(req.Messages)+len(req.ToolHistory)*2)
	for _, message := range req.Messages {
		if message.Role == "system" {
			// System prompts stay plain text (Anthropic top-level "system" field).
			if text := strings.TrimSpace(MessageText(message.Content)); text != "" {
				systems = append(systems, text)
			}
			continue
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": anthropicMessageContent(message.Content)})
	}
	for _, round := range toolRoundsForRequest(req) {
		uses := make([]map[string]any, 0, len(round))
		results := make([]map[string]any, 0, len(round))
		for _, exchange := range round {
			uses = append(uses, map[string]any{"type": "tool_use", "id": exchange.Call.ID, "name": publicToolName(exchange.Call.Name), "input": exchange.Call.Arguments})
			results = append(results, map[string]any{"type": "tool_result", "tool_use_id": exchange.Call.ID, "content": toolExchangeContent(exchange.Result), "is_error": exchange.Result.Status != tools.CallStatusCompleted})
		}
		if len(uses) > 0 {
			messages = append(messages, map[string]any{"role": "assistant", "content": uses}, map[string]any{"role": "user", "content": results})
		}
	}
	return strings.Join(systems, "\n\n"), messages
}

func anthropicTools(definitions []tools.Definition) []map[string]any {
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{"name": publicToolName(definition.Name), "description": definition.Description, "input_schema": definition.Parameters})
	}
	return items
}

type anthropicContent struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func completeAnthropicResponse(raw []byte, emit func(ProviderChunk) error) error {
	var response struct {
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
	var calls []tools.Call
	var text strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
		if block.Type == "tool_use" {
			calls = append(calls, tools.Call{ID: block.ID, Name: internalToolName(block.Name), Arguments: block.Input})
		}
	}
	if len(calls) > 0 {
		return emit(ProviderChunk{ToolCalls: calls})
	}
	if text.Len() > 0 {
		if err := emit(ProviderChunk{Delta: text.String()}); err != nil {
			return err
		}
	}
	return emit(ProviderChunk{Final: true})
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
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return err
		}
		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				calls[event.Index] = &anthropicStreamCall{index: event.Index, id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
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
