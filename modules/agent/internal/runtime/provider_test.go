package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestEchoProviderRequestsDiffAndPatchTools(t *testing.T) {
	cases := []struct {
		name string
		text string
		tool string
	}{
		{name: "diff", text: "diff file README.md old => new", tool: "workspace.diff_file"},
		{name: "patch", text: "apply patch --- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new", tool: "workspace.apply_patch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := echoToolCalls(ProviderRequest{
				RunID: "run_" + tc.name,
				Input: methods.ReplyInput{
					Text: tc.text,
				},
			})
			if len(calls) != 1 {
				t.Fatalf("expected one tool call, got %#v", calls)
			}
			if calls[0].Name != tc.tool {
				t.Fatalf("expected %s, got %#v", tc.tool, calls[0])
			}
		})
	}
}

func TestEchoProviderRequestsMemoryTools(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		tool    string
		risk    string
		argKey  string
		argWant string
	}{
		{name: "list", text: "list memory session", tool: "memory.list", risk: "medium", argKey: "scope", argWant: "session"},
		{name: "create_session", text: "remember session Prefer short answers", tool: "memory.create", risk: "high", argKey: "scope", argWant: "session"},
		{name: "create_project", text: "remember project Use go test", tool: "memory.create", risk: "high", argKey: "scope", argWant: "project"},
		{name: "delete", text: "delete memory mem_1", tool: "memory.delete", risk: "high", argKey: "id", argWant: "mem_1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := echoToolCalls(ProviderRequest{
				RunID: "run_memory_" + tc.name,
				Input: methods.ReplyInput{
					Text: tc.text,
				},
			})
			if len(calls) != 1 {
				t.Fatalf("expected one tool call, got %#v", calls)
			}
			if calls[0].Name != tc.tool || string(calls[0].Risk) != tc.risk {
				t.Fatalf("unexpected memory tool call: %#v", calls[0])
			}
			if calls[0].Arguments[tc.argKey] != tc.argWant {
				t.Fatalf("argument %s = %#v, want %q", tc.argKey, calls[0].Arguments[tc.argKey], tc.argWant)
			}
		})
	}
}

func TestEchoProviderUsesPerRunHTTPProviderOverride(t *testing.T) {
	var sawAuth string
	var sawModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		sawModel, _ = body["model"].(string)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"profile ok"}}]}`))
	}))
	defer server.Close()

	var chunks []ProviderChunk
	err := EchoProvider{}.Complete(context.Background(), ProviderRequest{
		RunID: "run_profile",
		Input: methods.ReplyInput{
			Text: "hello",
		},
		Options: methods.ReplyOptions{
			ProviderName:    "openai_compatible",
			ProviderBaseURL: server.URL,
			ProviderAPIKey:  "sk-profile",
			Model:           "profile-model",
		},
	}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer sk-profile" || sawModel != "profile-model" {
		t.Fatalf("provider override mismatch auth=%q model=%q", sawAuth, sawModel)
	}
	if len(chunks) != 2 || chunks[0].Delta != "profile ok" || !chunks[1].Final {
		t.Fatalf("unexpected override chunks: %#v", chunks)
	}
}

func TestOpenAICompatibleChatCompletionsURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "host root", baseURL: "https://api.example.com", want: "https://api.example.com/v1/chat/completions"},
		{name: "versioned base", baseURL: "https://api.example.com/step_plan/v1/", want: "https://api.example.com/step_plan/v1/chat/completions"},
		{name: "full endpoint", baseURL: "https://api.example.com/custom/chat/completions", want: "https://api.example.com/custom/chat/completions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := openAICompatibleChatCompletionsURL(test.baseURL); got != test.want {
				t.Fatalf("URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHTTPCompatibleProviderSendsMemoryAsSeparateSystemMessage(t *testing.T) {
	var messages []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		messages = body.Messages
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"memory ok"}}]}`))
	}))
	defer server.Close()

	provider := HTTPCompatibleProvider{
		baseURL: server.URL,
		model:   "test-model",
		client:  server.Client(),
	}
	var chunks []ProviderChunk
	err := provider.Complete(context.Background(), ProviderRequest{
		RunID: "run_memory_provider",
		Input: methods.ReplyInput{
			Text: "current user prompt",
		},
		Options: methods.ReplyOptions{
			MemoryContext: &methods.MemoryContext{
				Context: "Memory:\n- [project/fact] Build: Use npm test.",
				Items: []methods.MemoryItem{{
					ID:      "mem_1",
					Scope:   "project",
					Kind:    "fact",
					Title:   "Build",
					Content: "Use npm test.",
				}},
			},
		},
	}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 {
		t.Fatalf("messages = %#v, want system + user", messages)
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "Memory:\n- [project/fact] Build: Use npm test." {
		t.Fatalf("memory system message mismatch: %#v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "current user prompt" {
		t.Fatalf("user message was not preserved: %#v", messages[1])
	}
	if len(chunks) != 2 || chunks[0].Delta != "memory ok" || !chunks[1].Final {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestOpenAICompatibleMessagesInjectOrchestrationPolicyWhenSubagentToolAvailable(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "分析桌面端和服务端"},
		Tools: []tools.Definition{{Name: "subagent.run", Description: "run"}},
	})
	if len(messages) < 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[0]["content"]), "subagent.run") {
		t.Fatalf("expected orchestration system policy, got %#v", messages[0])
	}
	if messages[1]["role"] != "user" {
		t.Fatalf("expected user after policy, got %#v", messages[1])
	}

	without := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "hello"},
		Tools: []tools.Definition{{Name: "workspace.read_file"}},
	})
	if len(without) != 1 || without[0]["role"] != "user" {
		t.Fatalf("child/specialist without subagent tool should not get policy: %#v", without)
	}
}

func TestOpenAICompatibleMessagesIncludeConversationInOrderAndCurrentInputOnce(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Session: methods.ReplySession{
			Conversation: []methods.Message{
				{
					Role: "user",
					Content: []methods.ContentBlock{
						{Type: "text", Text: "first question"},
						{Type: "text", Text: "with more detail"},
					},
				},
				{
					Role:    "assistant",
					Content: []methods.ContentBlock{{Type: "text", Text: "first answer"}},
				},
				{
					Role:    "subagent",
					Content: []methods.ContentBlock{{Type: "text", Text: "planner detail"}},
				},
			},
		},
		Input: methods.ReplyInput{Text: "current question"},
		Options: methods.ReplyOptions{MemoryContext: &methods.MemoryContext{
			Context: "remember this",
		}},
		ToolHistory: []ToolExchange{{
			Call: tools.Call{
				ID:        "call_1",
				Name:      "workspace.read_file",
				Arguments: map[string]any{"path": "README.md"},
			},
			Result: tools.Result{
				ToolCallID: "call_1",
				Name:       "workspace.read_file",
				Status:     tools.CallStatusCompleted,
				Output:     "project readme",
			},
		}},
	})

	want := []struct {
		role    string
		content string
	}{
		{role: "system", content: "remember this"},
		{role: "user", content: "first question\nwith more detail"},
		{role: "assistant", content: "first answer"},
		{role: "user", content: "current question"},
	}
	if len(messages) != len(want)+2 {
		t.Fatalf("messages = %#v, want %d messages", messages, len(want)+2)
	}
	currentInputCount := 0
	for index, expected := range want {
		if messages[index]["role"] != expected.role || messages[index]["content"] != expected.content {
			t.Fatalf("messages[%d] = %#v, want role=%q content=%q", index, messages[index], expected.role, expected.content)
		}
		if messages[index]["role"] == "user" && messages[index]["content"] == "current question" {
			currentInputCount++
		}
	}
	if currentInputCount != 1 {
		t.Fatalf("current input appeared %d times, want once: %#v", currentInputCount, messages)
	}
	if messages[4]["role"] != "assistant" {
		t.Fatalf("tool call message = %#v, want assistant after current input", messages[4])
	}
	if messages[5]["role"] != "tool" || messages[5]["tool_call_id"] != "call_1" || messages[5]["content"] != "project readme" {
		t.Fatalf("tool result message = %#v, want call_1 result after assistant tool call", messages[5])
	}
}

func TestHTTPCompatibleProviderSendsToolsAfterToolHistory(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	defer server.Close()

	provider := HTTPCompatibleProvider{
		baseURL: server.URL,
		model:   "test-model",
		client:  server.Client(),
	}
	err := provider.Complete(context.Background(), ProviderRequest{
		RunID: "run_tools_after_history",
		Input: methods.ReplyInput{
			Text: "inspect the project",
		},
		Tools: []tools.Definition{{
			Name:        "workspace.read_file",
			Description: "Read a workspace file",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolHistory: []ToolExchange{{
			Call: tools.Call{
				ID:        "call_1",
				Name:      "workspace.list",
				Arguments: map[string]any{"path": "."},
			},
			Result: tools.Result{
				ToolCallID: "call_1",
				Name:       "workspace.list",
				Status:     tools.CallStatusCompleted,
				Output:     "README.md",
			},
		}},
	}, func(ProviderChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	definitions, ok := body["tools"].([]any)
	if !ok || len(definitions) != 1 {
		t.Fatalf("tools = %#v, want one definition", body["tools"])
	}
	if body["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %#v, want auto", body["tool_choice"])
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages = %#v, want user + assistant tool call + tool result", body["messages"])
	}
}

func TestHTTPCompatibleProviderStreamsContentDeltas(t *testing.T) {
	var sawStream bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		sawStream, _ = body["stream"].(bool)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := HTTPCompatibleProvider{
		baseURL: server.URL,
		model:   "test-model",
		stream:  true,
		client:  server.Client(),
	}
	var chunks []ProviderChunk
	err := provider.Complete(context.Background(), ProviderRequest{
		RunID: "run_stream",
		Input: methods.ReplyInput{
			Text: "hello",
		},
	}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawStream {
		t.Fatal("expected request stream flag to be true")
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %#v", chunks)
	}
	if chunks[0].Delta != "hello" || chunks[1].Delta != " world" || !chunks[2].Final {
		t.Fatalf("unexpected stream chunks: %#v", chunks)
	}
}

func TestHTTPCompatibleProviderStreamsToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"workspace__read_file\",\"arguments\":\"{\\\"path\\\":\\\"README\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\".md\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := HTTPCompatibleProvider{
		baseURL: server.URL,
		model:   "test-model",
		stream:  true,
		client:  server.Client(),
	}
	var chunks []ProviderChunk
	err := provider.Complete(context.Background(), ProviderRequest{
		RunID: "run_stream_tool",
		Input: methods.ReplyInput{
			Text: "read",
		},
	}, func(chunk ProviderChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || len(chunks[0].ToolCalls) != 1 {
		t.Fatalf("expected one tool-call chunk, got %#v", chunks)
	}
	call := chunks[0].ToolCalls[0]
	if call.ID != "call_1" || call.Name != "workspace.read_file" || call.Arguments["path"] != "README.md" {
		t.Fatalf("unexpected streamed tool call: %#v", call)
	}
}
