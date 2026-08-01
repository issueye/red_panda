package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/tools"
)

func TestOpenAICompatibleMessagesGolden(t *testing.T) {
	req := ProviderRequest{
		Prompt: PromptEnvelope{
			StablePrefix: []Message{{Role: "system", Content: "policy"}},
			History:      []Message{{Role: "user", Content: "first question"}, {Role: "assistant", Content: "first answer"}},
			TurnTail:     []Message{{Role: "user", Content: "current question"}},
		},
		ToolRounds: [][]ToolExchange{{
			{Call: tools.Call{ID: "call_1", Name: "workspace.read_file", Arguments: map[string]any{"path": "README.md"}}, Result: tools.Result{ToolCallID: "call_1", Name: "workspace.read_file", Status: tools.CallStatusCompleted, Output: "project readme"}},
			{Call: tools.Call{ID: "call_2", Name: "goal.assess", Arguments: map[string]any{"status": "continue"}}, Result: tools.Result{ToolCallID: "call_2", Name: "goal.assess", Status: tools.CallStatusFailed, Error: "not ready"}},
		}},
	}

	got, err := json.Marshal(openAICompatibleMessages(req))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"content":"policy","role":"system"},{"content":"first question","role":"user"},{"content":"first answer","role":"assistant"},{"content":"current question","role":"user"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{\"path\":\"README.md\"}","name":"workspace__read_file"},"id":"call_1","type":"function"},{"function":{"arguments":"{\"status\":\"continue\"}","name":"goal__assess"},"id":"call_2","type":"function"}]},{"content":"{\"schema\":\"red_panda.tool_result.v1\",\"tool\":\"workspace.read_file\",\"status\":\"completed\",\"ok\":true,\"text\":\"project readme\",\"meta\":{\"original_bytes\":14,\"note\":\"legacy output wrapped for model\"}}","name":"workspace__read_file","role":"tool","tool_call_id":"call_1"},{"content":"{\"schema\":\"red_panda.tool_result.v1\",\"tool\":\"goal.assess\",\"status\":\"failed\",\"ok\":false,\"text\":\"not ready\",\"error\":\"not ready\",\"meta\":{\"original_bytes\":9,\"note\":\"legacy output wrapped for model\"}}","name":"goal__assess","role":"tool","tool_call_id":"call_2"}]`
	if string(got) != want {
		t.Fatalf("serialized messages changed\ngot:  %s\nwant: %s", got, want)
	}
}

func TestToolRoundModelContentsSharesRoundBudget(t *testing.T) {
	round := []ToolExchange{
		{Result: tools.Result{Name: "workspace.read_file", Status: tools.CallStatusCompleted, Output: strings.Repeat("a", maxToolRoundModelBytes)}},
		{Result: tools.Result{Name: "workspace.read_file", Status: tools.CallStatusCompleted, Output: strings.Repeat("b", maxToolRoundModelBytes)}},
	}
	contents := toolRoundModelContents(round)
	if len(contents) != len(round) {
		t.Fatalf("contents = %d, want %d", len(contents), len(round))
	}
	if got := len(contents[0]) + len(contents[1]); got > maxToolRoundModelBytes {
		t.Fatalf("tool round model content = %d bytes, want <= %d", got, maxToolRoundModelBytes)
	}
}

func TestToolRoundModelContentsPreservesLargeWorkerReport(t *testing.T) {
	report := strings.Repeat("analysis-line\n", 3000) + "END-OF-WORKER-REPORT"
	raw, err := json.Marshal(map[string]any{
		"assignment_id": "assignment-1", "worker_id": "worker-02",
		"status": "completed", "output": report, "report_complete": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	contents := toolRoundModelContents([]ToolExchange{{
		Call:   tools.Call{Name: "worker.delegate"},
		Result: tools.Result{Name: "worker.delegate", Status: tools.CallStatusCompleted, Output: string(raw)},
	}})
	if len(contents) != 1 || !strings.Contains(contents[0], "END-OF-WORKER-REPORT") {
		t.Fatalf("large Worker report was truncated: bytes=%d tail=%v", len(contents[0]), strings.Contains(contents[0], "END-OF-WORKER-REPORT"))
	}
	if !strings.Contains(contents[0], "assignment-1") {
		t.Fatalf("Worker metadata missing: %s", contents[0][:min(512, len(contents[0]))])
	}
}

func TestEchoProviderUsesPerRunHTTPProviderOverride(t *testing.T) {
	var auth string
	var model string
	var stream bool
	var enableThinking bool
	var reasoningEffort string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		model, _ = body["model"].(string)
		stream, _ = body["stream"].(bool)
		enableThinking, _ = body["enable_thinking"].(bool)
		reasoningEffort, _ = body["reasoning_effort"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"profile ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	var chunks []ProviderChunk
	err := (EchoProvider{}).Complete(context.Background(), ProviderRequest{
		RunID: "run_profile", Input: "hello", Prompt: PromptEnvelope{TurnTail: []Message{{Role: "user", Content: "hello"}}},
		Options: RequestOptions{ProviderName: "openai_compatible", ProviderBaseURL: server.URL, ProviderAPIKey: "sk-profile", Model: "profile-model", EnableThinking: true, ReasoningEffort: "low"},
	}, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-profile" || model != "profile-model" || !stream || !enableThinking || reasoningEffort != "low" {
		t.Fatalf("override mismatch auth=%q model=%q stream=%v thinking=%v effort=%q", auth, model, stream, enableThinking, reasoningEffort)
	}
	if len(chunks) != 2 || chunks[0].Delta != "profile ok" || !chunks[1].Final {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestHTTPCompatibleProviderRetriesBeforeOutput(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer server.Close()

	p := HTTPCompatibleProvider{BaseURL: server.URL, Model: "model", Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}
	if err := p.Complete(context.Background(), ProviderRequest{Prompt: PromptEnvelope{TurnTail: []Message{{Role: "user", Content: "go"}}}}, func(ProviderChunk) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestHTTPCompatibleProviderRetriesUnauthorized(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, `{"error":{"message":"no enabled grok credential"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
	}))
	defer server.Close()

	p := HTTPCompatibleProvider{BaseURL: server.URL, Model: "model", Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}
	if err := p.Complete(context.Background(), ProviderRequest{Prompt: PromptEnvelope{TurnTail: []Message{{Role: "user", Content: "go"}}}}, func(ProviderChunk) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestIsRetryableProviderError(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "401", err: providerHTTPError{StatusCode: http.StatusUnauthorized, Body: "no cred"}, want: true},
		{name: "408", err: providerHTTPError{StatusCode: http.StatusRequestTimeout}, want: true},
		{name: "429", err: providerHTTPError{StatusCode: http.StatusTooManyRequests}, want: true},
		{name: "500", err: providerHTTPError{StatusCode: http.StatusInternalServerError}, want: true},
		{name: "400", err: providerHTTPError{StatusCode: http.StatusBadRequest, Body: "bad"}, want: false},
		{name: "403", err: providerHTTPError{StatusCode: http.StatusForbidden}, want: false},
		{name: "404", err: providerHTTPError{StatusCode: http.StatusNotFound}, want: false},
	}
	for _, tc := range cases {
		if got := isRetryableProviderError(ctx, tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if isRetryableProviderError(canceled, providerHTTPError{StatusCode: http.StatusUnauthorized}) {
		t.Fatal("canceled context must not retry")
	}
}

func TestHTTPCompatibleProviderDoesNotRetryAfterStreamOutput(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: invalid-json\n\n")
	}))
	defer server.Close()

	p := HTTPCompatibleProvider{BaseURL: server.URL, Model: "model", Stream: true, Client: server.Client(), MaxAttempts: 3, RetryBaseDelay: time.Millisecond}
	var chunks []ProviderChunk
	err := p.Complete(context.Background(), ProviderRequest{Prompt: PromptEnvelope{TurnTail: []Message{{Role: "user", Content: "go"}}}}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err == nil || attempts != 1 || len(chunks) != 1 || chunks[0].Delta != "partial" {
		t.Fatalf("err=%v attempts=%d chunks=%#v", err, attempts, chunks)
	}
}

func TestOpenAICompatibleChatCompletionsURL(t *testing.T) {
	for _, tt := range []struct{ base, want string }{
		{"https://api.example.com", "https://api.example.com/v1/chat/completions"},
		{"https://api.example.com/v1/", "https://api.example.com/v1/chat/completions"},
		{"https://api.example.com/custom/chat/completions", "https://api.example.com/custom/chat/completions"},
	} {
		if got := openAICompatibleChatCompletionsURL(tt.base); got != tt.want {
			t.Fatalf("URL(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestOpenAIUsageIncludesCachedTokens(t *testing.T) {
	var chunks []ProviderChunk
	raw := []byte(`{"choices":[{"message":{"content":"done"}}],"usage":{"prompt_tokens":120,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":96}}}`)
	if err := completeHTTPResponse(raw, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 1 || chunks[0].Usage == nil || chunks[0].Usage.CacheReadTokens != 96 || chunks[0].Usage.InputTokens != 120 {
		t.Fatalf("usage = %#v", chunks)
	}
}

func TestOpenAIUsageAcceptsTopLevelCompatibleCachedTokens(t *testing.T) {
	var chunks []ProviderChunk
	raw := []byte(`{"choices":[{"message":{"content":"done"}}],"usage":{"prompt_tokens":120,"completion_tokens":8,"cached_tokens":96}}`)
	if err := completeHTTPResponse(raw, func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 1 || chunks[0].Usage == nil || chunks[0].Usage.CacheReadTokens != 96 {
		t.Fatalf("usage = %#v", chunks)
	}
}

func TestHTTPCompatibleProviderSendsNeutralRequest(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer server.Close()

	p := HTTPCompatibleProvider{BaseURL: server.URL, Model: "fallback", Client: server.Client()}
	err := p.Complete(context.Background(), ProviderRequest{
		RunID: "run_1", SessionID: "session_1", Input: "inspect",
		Prompt:  PromptEnvelope{StablePrefix: []Message{{Role: "system", Content: "time"}}, TurnTail: []Message{{Role: "user", Content: "inspect"}}},
		Options: RequestOptions{Model: "selected"},
		Tools:   []tools.Definition{{Name: "workspace.read_file", Description: "Read", Parameters: map[string]any{"type": "object"}}},
	}, func(ProviderChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "selected" || body["tool_choice"] != "auto" || body["stream"] != false {
		t.Fatalf("unexpected request body: %#v", body)
	}
	if _, exists := body["enable_thinking"]; exists {
		t.Fatalf("disabled thinking must not add enable_thinking: %#v", body)
	}
	if _, exists := body["prompt_cache_key"]; exists {
		t.Fatalf("generic Chat endpoint must not receive cache fields: %#v", body)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", body["messages"])
	}
}

func TestValidateCacheOptionsRejectsUnsupportedExplicitFields(t *testing.T) {
	if err := validateCacheOptions(RequestOptions{ProviderName: "openai_compatible", CacheMode: CacheModeExplicit}); err == nil {
		t.Fatal("generic explicit cache mode should be rejected")
	}
	if err := validateCacheOptions(RequestOptions{ProviderName: "openai_responses", CacheMode: CacheModeExplicit}); err == nil {
		t.Fatal("Responses explicit mode without cache-key support should be rejected")
	}
	if err := validateCacheOptions(RequestOptions{ProviderName: "openai_responses", CacheMode: CacheModeExplicit, CacheKeySupported: true, CacheRetention: "24h"}); err != nil {
		t.Fatalf("supported Responses cache options rejected: %v", err)
	}
	if err := validateCacheOptions(RequestOptions{ProviderName: "anthropic", CacheMode: CacheModeExplicit}); err != nil {
		t.Fatalf("Anthropic explicit cache mode rejected: %v", err)
	}
}

func TestEchoProviderPreservesInputAndToolBehavior(t *testing.T) {
	tests := []struct {
		input string
		name  string
	}{
		{input: "read file README.md", name: "workspace.read_file"},
		{input: "list memory project", name: "memory.list"},
		{input: "todo first; second", name: "todo.write"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := echoToolCalls(ProviderRequest{RunID: "run", Input: tt.input})
			if len(calls) != 1 || calls[0].Name != tt.name {
				t.Fatalf("calls = %#v", calls)
			}
		})
	}

	var chunks []ProviderChunk
	if err := (EchoProvider{}).Complete(context.Background(), ProviderRequest{Input: "hello"}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Delta != "hello" || !chunks[1].Final {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestStreamAndNonStreamSingleToolCallNormalizeIdentically(t *testing.T) {
	nonStream := `{"choices":[{"message":{"tool_calls":[{"id":"call_read","function":{"name":"workspace__read_file","arguments":"{\"path\":\"README.md\"}"}}]}}]}`
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_read","function":{"name":"workspace__read_file","arguments":"{\"path\":\"README"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":".md\"}"}}]}}]}`,
		`data: [DONE]`, "",
	}, "\n\n")

	collect := func(complete func(func(ProviderChunk) error) error) []ProviderChunk {
		var chunks []ProviderChunk
		if err := complete(func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		return chunks
	}
	a := collect(func(emit func(ProviderChunk) error) error { return completeHTTPResponse([]byte(nonStream), emit) })
	b := collect(func(emit func(ProviderChunk) error) error {
		return (HTTPCompatibleProvider{}).completeStream(strings.NewReader(stream), emit)
	})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("normalized calls differ\nnon-stream: %s\nstream: %s", fmt.Sprint(a), fmt.Sprint(b))
	}
}

func TestStreamAndNonStreamReasoningNormalizeIdentically(t *testing.T) {
	nonStream := `{"choices":[{"message":{"reasoning_content":"inspect first","content":"done"}}]}`
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"inspect first"}}]}`,
		`data: {"choices":[{"delta":{"content":"done"}}]}`,
		`data: [DONE]`, "",
	}, "\n\n")

	collect := func(complete func(func(ProviderChunk) error) error) []ProviderChunk {
		var chunks []ProviderChunk
		if err := complete(func(chunk ProviderChunk) error { chunks = append(chunks, chunk); return nil }); err != nil {
			t.Fatal(err)
		}
		return chunks
	}
	a := collect(func(emit func(ProviderChunk) error) error { return completeHTTPResponse([]byte(nonStream), emit) })
	b := collect(func(emit func(ProviderChunk) error) error {
		return (HTTPCompatibleProvider{}).completeStream(strings.NewReader(stream), emit)
	})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("normalized reasoning differs\nnon-stream: %s\nstream: %s", fmt.Sprint(a), fmt.Sprint(b))
	}
	if len(a) != 3 || a[0].ReasoningDelta != "inspect first" || a[1].Delta != "done" || !a[2].Final {
		t.Fatalf("chunks = %#v", a)
	}
}
