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

func TestOpenAIResponsesProviderRequestAndNonStreamToolCall(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `{"output":[{"type":"function_call","call_id":"call_2","name":"workspace__read_file","arguments":"{\"path\":\"README.md\"}"}]}`)
	}))
	defer server.Close()

	p := OpenAIResponsesProvider{providerConfig{BaseURL: server.URL, APIKey: "secret", Model: "fallback", Client: server.Client()}}
	var chunks []ProviderChunk
	err := p.Complete(context.Background(), Request{
		Messages:    []Message{{Role: "system", Content: "be direct"}, {Role: "user", Content: "inspect"}},
		Options:     RequestOptions{Model: "gpt-test", EnableThinking: true, ReasoningEffort: "high"},
		Tools:       []tools.Definition{{Name: "workspace.read_file", Description: "Read", Parameters: map[string]any{"type": "object"}}},
		ToolHistory: []ToolExchange{{Call: tools.Call{ID: "call_1", Name: "workspace.list", Arguments: map[string]any{"path": "."}}, Result: tools.Result{Status: tools.CallStatusCompleted, Output: "README.md"}}},
	}, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-test" || body["tool_choice"] != "auto" || body["stream"] != false {
		t.Fatalf("body = %#v", body)
	}
	if body["reasoning"].(map[string]any)["effort"] != "high" {
		t.Fatalf("reasoning = %#v", body["reasoning"])
	}
	input := body["input"].([]any)
	if len(input) != 4 || input[2].(map[string]any)["type"] != "function_call" || input[3].(map[string]any)["type"] != "function_call_output" {
		t.Fatalf("input = %#v", input)
	}
	definitions := body["tools"].([]any)
	if definitions[0].(map[string]any)["name"] != "workspace__read_file" {
		t.Fatalf("tools = %#v", definitions)
	}
	want := []ProviderChunk{{ToolCalls: []tools.Call{{
		ID: "call_2", Name: "workspace.read_file", Arguments: map[string]any{"path": "README.md"},
	}}}}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestOpenAIResponsesProviderStreamingTextAndToolArguments(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hel"}`,
			`data: {"type":"response.output_text.delta","delta":"lo"}`,
			`data: {"type":"response.completed"}`, "",
		}, "\n\n")
		var chunks []ProviderChunk
		if err := completeOpenAIResponsesStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(chunks) != 3 || chunks[0].Delta != "hel" || chunks[1].Delta != "lo" || !chunks[2].Final {
			t.Fatalf("chunks = %#v", chunks)
		}
	})

	t.Run("tool", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"workspace__read_file","arguments":""}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"README"}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":".md\"}"}`, "",
		}, "\n\n")
		var chunks []ProviderChunk
		if err := completeOpenAIResponsesStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(chunks) != 1 || chunks[0].ToolCalls[0].Name != "workspace.read_file" || chunks[0].ToolCalls[0].Arguments["path"] != "README.md" {
			t.Fatalf("chunks = %#v", chunks)
		}
	})
}

func TestOpenAIResponsesStreamUsageIncludesCachedTokens(t *testing.T) {
	stream := "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":200,\"output_tokens\":10,\"input_tokens_details\":{\"cached_tokens\":160}}}}\n\n"
	var chunks []ProviderChunk
	if err := completeOpenAIResponsesStream(strings.NewReader(stream), func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Usage == nil || chunks[0].Usage.CacheReadTokens != 160 || !chunks[1].Final {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestOpenAIResponsesProviderRetryBoundary(t *testing.T) {
	t.Run("before output", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts++
			if attempts < 3 {
				http.Error(w, "retry", http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}]}`)
		}))
		defer server.Close()
		p := OpenAIResponsesProvider{providerConfig{BaseURL: server.URL, Model: "m", Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}}
		if err := p.Complete(context.Background(), Request{}, func(ProviderChunk) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if attempts != 3 {
			t.Fatalf("attempts = %d", attempts)
		}
	})

	t.Run("after output", func(t *testing.T) {
		attempts := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: invalid\n\n")
		}))
		defer server.Close()
		p := OpenAIResponsesProvider{providerConfig{BaseURL: server.URL, Model: "m", Stream: true, Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}}
		err := p.Complete(context.Background(), Request{}, func(ProviderChunk) error { return nil })
		if err == nil || attempts != 1 {
			t.Fatalf("err = %v, attempts = %d", err, attempts)
		}
	})
}
