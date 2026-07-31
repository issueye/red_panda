package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/tools"
)

func TestAnthropicProviderRequestAndNonStreamToolUse(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("headers = %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","id":"call_2","name":"workspace__read_file","input":{"path":"README.md"}}]}`)
	}))
	defer server.Close()

	p := AnthropicProvider{providerConfig{BaseURL: server.URL, APIKey: "secret", Model: "claude-test", Client: server.Client()}}
	var chunks []ProviderChunk
	err := p.Complete(context.Background(), Request{
		Prompt:      PromptEnvelope{StablePrefix: []Message{{Role: "system", Content: "be direct"}}, TurnTail: []Message{{Role: "user", Content: "inspect"}}},
		Options:     RequestOptions{EnableThinking: true, ReasoningEffort: "xhigh"},
		Tools:       []tools.Definition{{Name: "workspace.read_file", Description: "Read", Parameters: map[string]any{"type": "object"}}},
		ToolHistory: []ToolExchange{{Call: tools.Call{ID: "call_1", Name: "workspace.list", Arguments: map[string]any{"path": "."}}, Result: tools.Result{Status: tools.CallStatusFailed, Error: "denied"}}},
	}, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil })
	if err != nil {
		t.Fatal(err)
	}
	system := body["system"].([]any)
	if system[0].(map[string]any)["text"] != "be direct" || system[0].(map[string]any)["cache_control"].(map[string]any)["type"] != "ephemeral" || body["max_tokens"] != float64(defaultAnthropicMaxTokens) || body["tool_choice"].(map[string]any)["type"] != "auto" {
		t.Fatalf("body = %#v", body)
	}
	if body["output_config"].(map[string]any)["effort"] != "max" {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
	messages := body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	toolUse := messages[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	toolResult := messages[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolResult["type"] != "tool_result" || toolResult["is_error"] != true {
		t.Fatalf("history = %#v %#v", toolUse, toolResult)
	}
	definition := body["tools"].([]any)[0].(map[string]any)
	if definition["name"] != "workspace__read_file" || definition["input_schema"] == nil || definition["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("tool = %#v", definition)
	}
	want := []ProviderChunk{{ToolCalls: []tools.Call{{
		ID: "call_2", Name: "workspace.read_file", Arguments: map[string]any{"path": "README.md"},
	}}}}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestAnthropicCacheBreakpointsCanBeDisabled(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()
	p := AnthropicProvider{providerConfig{BaseURL: server.URL, Model: "m", Client: server.Client()}}
	err := p.Complete(context.Background(), Request{
		Prompt:  PromptEnvelope{StablePrefix: []Message{{Role: "system", Content: "stable"}}, TurnTail: []Message{{Role: "user", Content: "hello"}}},
		Options: RequestOptions{CacheMode: CacheModeDisabled},
		Tools:   []tools.Definition{{Name: "workspace.read_file", Parameters: map[string]any{"type": "object"}}},
	}, func(ProviderChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	system := body["system"].([]any)
	if _, ok := system[0].(map[string]any)["cache_control"]; ok {
		t.Fatalf("system cache breakpoint was not disabled: %#v", system)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if _, ok := tool["cache_control"]; ok {
		t.Fatalf("tool cache breakpoint was not disabled: %#v", tool)
	}
}

func TestAnthropicProviderStreamingTextAndToolInput(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			`data: {"type":"message_stop"}`, "",
		}, "\n\n")
		var chunks []ProviderChunk
		if err := completeAnthropicStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(chunks) != 2 || chunks[0].Delta != "hello" || !chunks[1].Final {
			t.Fatalf("chunks = %#v", chunks)
		}
	})

	t.Run("tool", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"workspace__read_file","input":{}}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"README"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":".md\"}"}}`,
			`data: {"type":"message_stop"}`, "",
		}, "\n\n")
		var chunks []ProviderChunk
		if err := completeAnthropicStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(chunks) != 1 || chunks[0].ToolCalls[0].Name != "workspace.read_file" || chunks[0].ToolCalls[0].Arguments["path"] != "README.md" {
			t.Fatalf("chunks = %#v", chunks)
		}
	})
}

func TestAnthropicStreamUsageIncludesCacheTokens(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}}`,
		`data: {"type":"message_delta","usage":{"output_tokens":7}}`, "",
	}, "\n\n")
	var chunks []ProviderChunk
	if err := completeAnthropicStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 || chunks[0].Usage == nil || chunks[0].Usage.CacheReadTokens != 80 || chunks[0].Usage.CacheWriteTokens != 20 || chunks[1].Usage.OutputTokens != 7 || !chunks[2].Final {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestAnthropicProviderRetryBoundary(t *testing.T) {
	t.Run("before output", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts++
			if attempts < 2 {
				http.Error(w, "retry", http.StatusTooManyRequests)
				return
			}
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"done"}]}`)
		}))
		defer server.Close()
		p := AnthropicProvider{providerConfig{BaseURL: server.URL, Model: "m", Client: server.Client(), MaxAttempts: 2, RetryBaseDelay: time.Millisecond}}
		if err := p.Complete(context.Background(), Request{}, func(ProviderChunk) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if attempts != 2 {
			t.Fatalf("attempts = %d", attempts)
		}
	})

	t.Run("after output", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts++
			_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\ndata: invalid\n\n")
		}))
		defer server.Close()
		p := AnthropicProvider{providerConfig{BaseURL: server.URL, Model: "m", Stream: true, Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}}
		err := p.Complete(context.Background(), Request{}, func(ProviderChunk) error { return nil })
		if err == nil || attempts != 1 {
			t.Fatalf("err = %v, attempts = %d", err, attempts)
		}
	})
}
