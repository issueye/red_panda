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
		Messages:    []Message{{Role: "system", Content: "be direct"}, {Role: "user", Content: "inspect"}},
		Options:     RequestOptions{EnableThinking: true, ReasoningEffort: "xhigh"},
		Tools:       []tools.Definition{{Name: "workspace.read_file", Description: "Read", Parameters: map[string]any{"type": "object"}}},
		ToolHistory: []ToolExchange{{Call: tools.Call{ID: "call_1", Name: "workspace.list", Arguments: map[string]any{"path": "."}}, Result: tools.Result{Status: tools.CallStatusFailed, Error: "denied"}}},
	}, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if body["system"] != "be direct" || body["max_tokens"] != float64(defaultAnthropicMaxTokens) || body["tool_choice"].(map[string]any)["type"] != "auto" {
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
	if definition["name"] != "workspace__read_file" || definition["input_schema"] == nil {
		t.Fatalf("tool = %#v", definition)
	}
	want := []ProviderChunk{{ToolCalls: []tools.Call{{
		ID: "call_2", Name: "workspace.read_file", Arguments: map[string]any{"path": "README.md"},
	}}}}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v", chunks)
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
