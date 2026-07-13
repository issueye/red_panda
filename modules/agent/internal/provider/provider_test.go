package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestOpenAICompatibleMessagesInjectsCurrentTime(t *testing.T) {
	now := time.Now()
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "现在几点"},
	})
	if len(messages) < 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0]["role"] != "system" {
		t.Fatalf("expected time system message first, got %#v", messages[0])
	}
	content := fmt.Sprint(messages[0]["content"])
	if !strings.Contains(content, "当前权威时间") || !strings.Contains(content, now.Format("2006-01-02")) {
		t.Fatalf("time system message missing date: %q", content)
	}
	if messages[1]["role"] != "user" {
		t.Fatalf("expected user after time, got %#v", messages[1])
	}
}

func TestCurrentTimeContextMessageIncludesZone(t *testing.T) {
	// 若可用则固定为 Asia/Shanghai 时刻，否则使用本地时区。
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 11, 16, 30, 0, 0, loc)
	msg := currentTimeContextMessage(now)
	if !strings.Contains(msg, "2026-01-02") && !strings.Contains(msg, "2026-07-11") {
		t.Fatalf("expected date in message: %q", msg)
	}
	if !strings.Contains(msg, "16:30:00") || !strings.Contains(msg, "UTC+08:00") {
		t.Fatalf("expected time/offset in message: %q", msg)
	}
	if !strings.Contains(msg, "星期六") {
		t.Fatalf("expected weekday: %q", msg)
	}
}

func TestOpenAICompatibleMessagesInjectsSkillsCatalog(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "run code review skill"},
		Options: methods.ReplyOptions{
			SkillsContext: &methods.SkillsContext{
				Context: "Managed skills catalog\n- code-review: Review code safely.",
				Items: []methods.SkillSummary{{
					Name:        "code-review",
					Description: "Review code safely.",
				}},
			},
		},
	})
	if len(messages) < 3 {
		t.Fatalf("messages = %#v", messages)
	}
	// messages[0] 为当前时间，后续为技能目录。
	if messages[1]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[1]["content"]), "code-review") {
		t.Fatalf("expected skills system catalog, got %#v", messages[1])
	}
	if messages[2]["role"] != "user" {
		t.Fatalf("expected user after skills catalog, got %#v", messages[2])
	}
}

func TestOpenAICompatibleMessagesPutsSpecialistContextBeforeMemory(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "do the step"},
		Options: methods.ReplyOptions{
			SpecialistContext: &methods.SpecialistContext{
				Kind:    "specialist",
				Context: "You are goal-analyst. SPECIALIST_ROLE",
			},
			MemoryContext: &methods.MemoryContext{
				Context: "Memory:\n- [project/fact] Prefer tests.",
			},
		},
	})
	// [0]=时间，[1]=专业角色，[2]=记忆，[3]=用户。
	if len(messages) < 4 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[1]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[1]["content"]), "SPECIALIST_ROLE") {
		t.Fatalf("expected specialist system message, got %#v", messages[1])
	}
	if messages[2]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[2]["content"]), "Prefer tests") {
		t.Fatalf("expected memory after specialist, got %#v", messages[2])
	}
	if messages[3]["role"] != "user" {
		t.Fatalf("expected user last, got %#v", messages[3])
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
		BaseURL: server.URL,
		Model:   "test-model",
		Client:  server.Client(),
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
	if len(messages) < 3 {
		t.Fatalf("messages = %#v, want time + memory + user", messages)
	}
	if messages[0]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[0]["content"]), "当前权威时间") {
		t.Fatalf("expected time system message first, got %#v", messages[0])
	}
	if messages[1]["role"] != "system" || messages[1]["content"] != "Memory:\n- [project/fact] Build: Use npm test." {
		t.Fatalf("memory system message mismatch: %#v", messages[1])
	}
	if messages[2]["role"] != "user" || messages[2]["content"] != "current user prompt" {
		t.Fatalf("user message was not preserved: %#v", messages[2])
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
	if len(messages) < 3 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[0]["content"]), "当前权威时间") {
		t.Fatalf("expected time system message first, got %#v", messages[0])
	}
	policy := fmt.Sprint(messages[1]["content"])
	if messages[1]["role"] != "system" || !strings.Contains(policy, "subagent.run") {
		t.Fatalf("expected orchestration system policy, got %#v", messages[1])
	}
	if !strings.Contains(policy, "foundation specialist") || !strings.Contains(policy, "main.go") {
		t.Fatalf("expected root/config files to have explicit specialist ownership: %q", policy)
	}
	if messages[2]["role"] != "user" {
		t.Fatalf("expected user after policy, got %#v", messages[2])
	}

	without := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "hello"},
		Tools: []tools.Definition{{Name: "workspace.read_file"}},
	})
	if len(without) != 2 || without[0]["role"] != "system" || without[1]["role"] != "user" {
		t.Fatalf("child/specialist without subagent/todo tools should get time + user only: %#v", without)
	}
	if strings.Contains(fmt.Sprint(without[0]["content"]), "subagent.run") {
		t.Fatalf("child/specialist without subagent tool should not get orchestration policy: %#v", without[0])
	}
}

func TestGoalPipelineDoesNotInjectParallelOrchestrationPolicy(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "分析并实现项目改动"},
		Tools: []tools.Definition{
			{Name: "subagent.run"},
			{Name: "goal.write"},
			{Name: "todo.write"},
		},
	})

	joined := fmt.Sprint(messages)
	if strings.Contains(joined, "spawn multiple subagent.run calls IN ONE TURN") {
		t.Fatalf("goal pipeline must not receive parallel survey policy: %s", joined)
	}
	if !strings.Contains(joined, "exactly ONE specialist") || !strings.Contains(joined, "pipeline_phase") {
		t.Fatalf("goal pipeline should enforce sequential specialists: %s", joined)
	}
}

func TestOpenAICompatibleMessagesInjectPostDelegationPolicy(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "汇总项目分析"},
		Tools: []tools.Definition{
			{Name: "subagent.run"},
			{Name: "workspace.read_file"},
		},
		ToolHistory: []ToolExchange{{
			Call: tools.Call{
				ID:        "call_foundation",
				Name:      "subagent.run",
				Arguments: map[string]any{"name": "foundation", "path": "."},
			},
			Result: tools.Result{
				ToolCallID: "call_foundation",
				Name:       "subagent.run",
				Status:     tools.CallStatusCompleted,
				Output:     "main.go starts Wails; wails.json defines the frontend build.",
			},
		}},
	})

	if len(messages) < 5 {
		t.Fatalf("messages = %#v", messages)
	}
	if got := fmt.Sprint(messages[2]["content"]); messages[2]["role"] != "system" ||
		!strings.Contains(got, "Post-delegation rule") ||
		!strings.Contains(got, "Do not call workspace.read_file") {
		t.Fatalf("expected post-delegation synthesis policy, got %#v", messages[2])
	}

	failed := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "继续"},
		Tools: []tools.Definition{{Name: "subagent.run"}},
		ToolHistory: []ToolExchange{{
			Call:   tools.Call{ID: "call_failed", Name: "subagent.run"},
			Result: tools.Result{Status: tools.CallStatusFailed},
		}},
	})
	for _, message := range failed {
		if strings.Contains(fmt.Sprint(message["content"]), "Post-delegation rule") {
			t.Fatalf("failed subagent must not trigger successful-report policy: %#v", failed)
		}
	}
}

func TestOpenAICompatibleMessagesInjectTodoPolicyWhenTodoWriteAvailable(t *testing.T) {
	messages := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "实现登录并写测试"},
		Tools: []tools.Definition{
			{Name: "todo.write", Description: "update todos"},
			{Name: "workspace.read_file"},
		},
	})
	if len(messages) < 3 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[0]["content"]), "当前权威时间") {
		t.Fatalf("expected time first, got %#v", messages[0])
	}
	if messages[1]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[1]["content"]), "todo.write") {
		t.Fatalf("expected todo policy system message, got %#v", messages[1])
	}
	if messages[2]["role"] != "user" {
		t.Fatalf("expected user after todo policy, got %#v", messages[2])
	}

	withBoth := openAICompatibleMessages(ProviderRequest{
		Input: methods.ReplyInput{Text: "大范围重构"},
		Tools: []tools.Definition{
			{Name: "subagent.run"},
			{Name: "todo.write"},
		},
		Options: methods.ReplyOptions{
			TodoContext: &methods.TodoContext{
				Context: "Current session task list:\n- [in_progress] 拆分模块 (1)",
			},
		},
	})
	// 时间、编排策略、待办策略、待办上下文、用户。
	if len(withBoth) < 5 {
		t.Fatalf("messages = %#v", withBoth)
	}
	joined := fmt.Sprint(withBoth[1]["content"]) + fmt.Sprint(withBoth[2]["content"]) + fmt.Sprint(withBoth[3]["content"])
	if !strings.Contains(joined, "subagent.run") || !strings.Contains(joined, "todo.write") {
		t.Fatalf("expected orchestration + todo policy when both tools available: %#v", withBoth)
	}
	if !strings.Contains(fmt.Sprint(withBoth[3]["content"]), "in_progress") {
		t.Fatalf("expected live todo context after policies: %#v", withBoth[3])
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

	// [0]=时间系统提示，[1]=记忆系统提示，之后为会话、当前输入和工具交互。
	want := []struct {
		role    string
		content string
	}{
		{role: "system", content: "remember this"},
		{role: "user", content: "first question\nwith more detail"},
		{role: "assistant", content: "first answer"},
		{role: "user", content: "current question"},
	}
	if len(messages) != 1+len(want)+2 {
		t.Fatalf("messages = %#v, want %d messages", messages, 1+len(want)+2)
	}
	if messages[0]["role"] != "system" || !strings.Contains(fmt.Sprint(messages[0]["content"]), "当前权威时间") {
		t.Fatalf("messages[0] should be time system message: %#v", messages[0])
	}
	currentInputCount := 0
	for index, expected := range want {
		msg := messages[index+1]
		if msg["role"] != expected.role || msg["content"] != expected.content {
			t.Fatalf("messages[%d] = %#v, want role=%q content=%q", index+1, msg, expected.role, expected.content)
		}
		if msg["role"] == "user" && msg["content"] == "current question" {
			currentInputCount++
		}
	}
	if currentInputCount != 1 {
		t.Fatalf("current input appeared %d times, want once: %#v", currentInputCount, messages)
	}
	if messages[5]["role"] != "assistant" {
		t.Fatalf("tool call message = %#v, want assistant after current input", messages[5])
	}
	if messages[6]["role"] != "tool" || messages[6]["tool_call_id"] != "call_1" {
		t.Fatalf("tool result message = %#v, want call_1 result after assistant tool call", messages[6])
	}
	content := fmt.Sprint(messages[6]["content"])
	if !strings.Contains(content, "project readme") || !strings.Contains(content, "red_panda.tool_result.v1") {
		t.Fatalf("tool result should be standardized envelope containing project readme: %q", content)
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
		BaseURL: server.URL,
		Model:   "test-model",
		Client:  server.Client(),
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
	if !ok || len(messages) != 4 {
		t.Fatalf("messages = %#v, want time + user + assistant tool call + tool result", body["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(fmt.Sprint(first["content"]), "当前权威时间") {
		t.Fatalf("first message should be time system note: %#v", messages[0])
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
		BaseURL: server.URL,
		Model:   "test-model",
		Stream:  true,
		Client:  server.Client(),
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
		BaseURL: server.URL,
		Model:   "test-model",
		Stream:  true,
		Client:  server.Client(),
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
