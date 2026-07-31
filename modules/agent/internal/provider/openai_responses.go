package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"redpanda/protocol/tools"
)

type OpenAIResponsesProvider struct{ providerConfig }

func (OpenAIResponsesProvider) Name() string { return "openai_responses" }

func (p OpenAIResponsesProvider) Complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	if override, ok := Resolve(req.Options, p.Stream); ok {
		return completeResolvedProvider(ctx, override, req, emit)
	}
	return p.complete(ctx, req, emit)
}

func (p OpenAIResponsesProvider) complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	return completeWithRetry(ctx, p.providerConfig, req, emit, p.completeAttempt)
}

func (p OpenAIResponsesProvider) completeAttempt(ctx context.Context, req Request, emit func(ProviderChunk) error) error {
	model := req.Options.Model
	if model == "" {
		model = p.Model
	}
	body := map[string]any{"model": model, "input": openAIResponsesInput(req), "stream": p.Stream}
	if req.Options.EnableThinking {
		reasoning := map[string]any{"summary": "auto"}
		if effort := strings.TrimSpace(req.Options.ReasoningEffort); effort != "" {
			reasoning["effort"] = effort
		}
		body["reasoning"] = reasoning
	}
	if len(req.Tools) > 0 {
		body["tools"] = openAIResponsesTools(req.Tools)
		body["tool_choice"] = "auto"
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := openAIResponsesURL(p.BaseURL)
	logProviderRequest(req, model, endpoint, rawBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
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
		return completeOpenAIResponsesStream(resp.Body, emit)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return completeOpenAIResponsesResponse(raw, emit)
}

func openAIResponsesURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(base, "/responses") {
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/responses"
	}
	return base + "/v1/responses"
}

func openAIResponsesInput(req Request) []map[string]any {
	items := make([]map[string]any, 0, len(req.Messages)+len(req.ToolHistory)*2)
	for _, message := range req.Messages {
		items = append(items, map[string]any{"role": message.Role, "content": openAIResponsesContent(message.Content)})
	}
	for _, round := range toolRoundsForRequest(req) {
		for _, exchange := range round {
			arguments, _ := json.Marshal(exchange.Call.Arguments)
			items = append(items, map[string]any{
				"type": "function_call", "call_id": exchange.Call.ID,
				"name": publicToolName(exchange.Call.Name), "arguments": string(arguments),
			})
		}
		for _, exchange := range round {
			items = append(items, map[string]any{
				"type": "function_call_output", "call_id": exchange.Call.ID,
				"output": toolExchangeContent(exchange.Result),
			})
		}
	}
	return items
}

func openAIResponsesTools(definitions []tools.Definition) []map[string]any {
	items := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, map[string]any{
			"type": "function", "name": publicToolName(definition.Name),
			"description": definition.Description, "parameters": definition.Parameters,
		})
	}
	return items
}

type responsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func completeOpenAIResponsesResponse(raw []byte, emit func(ProviderChunk) error) error {
	var response struct {
		Output []responsesOutputItem `json:"output"`
		Error  *struct {
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
	var reasoning strings.Builder
	for _, item := range response.Output {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				if content.Type == "output_text" {
					text.WriteString(content.Text)
				}
			}
		case "function_call":
			calls = append(calls, responseToolCall(item.CallID, item.Name, item.Arguments))
		case "reasoning":
			for _, content := range item.Content {
				if content.Type == "summary_text" {
					reasoning.WriteString(content.Text)
				}
			}
		}
	}
	if reasoning.Len() > 0 {
		if err := emit(ProviderChunk{ReasoningDelta: reasoning.String()}); err != nil {
			return err
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

type responsesStreamCall struct {
	index     int
	id, name  string
	arguments strings.Builder
}

func completeOpenAIResponsesStream(reader io.Reader, emit func(ProviderChunk) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	calls := map[int]*responsesStreamCall{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type        string              `json:"type"`
			Delta       string              `json:"delta"`
			OutputIndex int                 `json:"output_index"`
			Item        responsesOutputItem `json:"item"`
			Error       *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return err
		}
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				if err := emit(ProviderChunk{Delta: event.Delta}); err != nil {
					return err
				}
			}
		case "response.reasoning_summary_text.delta":
			if event.Delta != "" {
				if err := emit(ProviderChunk{ReasoningDelta: event.Delta}); err != nil {
					return err
				}
			}
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				calls[event.OutputIndex] = &responsesStreamCall{index: event.OutputIndex, id: event.Item.CallID, name: event.Item.Name}
				calls[event.OutputIndex].arguments.WriteString(event.Item.Arguments)
			}
		case "response.function_call_arguments.delta":
			call := calls[event.OutputIndex]
			if call == nil {
				call = &responsesStreamCall{index: event.OutputIndex}
				calls[event.OutputIndex] = call
			}
			call.arguments.WriteString(event.Delta)
		case "response.output_item.done":
			if event.Item.Type == "function_call" {
				call := calls[event.OutputIndex]
				if call == nil {
					call = &responsesStreamCall{index: event.OutputIndex}
					calls[event.OutputIndex] = call
				}
				if event.Item.CallID != "" {
					call.id = event.Item.CallID
				}
				if event.Item.Name != "" {
					call.name = event.Item.Name
				}
				if call.arguments.Len() == 0 {
					call.arguments.WriteString(event.Item.Arguments)
				}
			}
		case "error", "response.failed":
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
			if call.name != "" {
				items = append(items, responseToolCall(call.id, call.name, call.arguments.String()))
			}
		}
		if len(items) > 0 {
			return emit(ProviderChunk{ToolCalls: items})
		}
	}
	return emit(ProviderChunk{Final: true})
}

func responseToolCall(id, name, arguments string) tools.Call {
	args := map[string]any{}
	if arguments != "" {
		_ = json.Unmarshal([]byte(arguments), &args)
	}
	return tools.Call{ID: id, Name: internalToolName(name), Arguments: args}
}

func logProviderRequest(req Request, model, endpoint string, body []byte) {
	if path, err := logLLMRequest(req.Options.LogLLMRequests, req.RunID, req.SessionID, model, endpoint, body); err != nil {
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request log failed: %v\n", err)
	} else if path != "" {
		fmt.Fprintf(os.Stderr, "red-panda-agent: llm request logged to %s\n", path)
	}
}
