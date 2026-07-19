package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

func encodeMessageText(t *testing.T, text string) string {
	t.Helper()
	raw, err := json.Marshal([]methods.ContentBlock{{Type: "text", Text: text}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPlanCompactionByTurnsKeepsRecentUserRounds(t *testing.T) {
	all := []model.Message{
		{Seq: 1, Role: "user", ContentJSON: encodeMessageText(t, "goal A")},
		{Seq: 2, Role: "assistant", ContentJSON: encodeMessageText(t, "did A")},
		{Seq: 3, Role: "user", ContentJSON: encodeMessageText(t, "goal B")},
		{Seq: 4, Role: "assistant", ContentJSON: encodeMessageText(t, "did B")},
		{Seq: 5, Role: "user", ContentJSON: encodeMessageText(t, "goal C")},
		{Seq: 6, Role: "assistant", ContentJSON: encodeMessageText(t, "did C")},
		{Seq: 7, Role: "user", ContentJSON: encodeMessageText(t, "goal D")},
		{Seq: 8, Role: "assistant", ContentJSON: encodeMessageText(t, "did D")},
	}
	plan := planCompactionByTurns(all, 0, 2)
	// Keep last 2 user turns: C+D (seq 5-8)
	if plan.KeepTailTurns != 2 || len(plan.TailMessages) != 4 {
		t.Fatalf("tail mismatch: turns=%d tail=%d plan=%#v", plan.KeepTailTurns, len(plan.TailMessages), plan)
	}
	if plan.TailMessages[0].Seq != 5 {
		t.Fatalf("tail starts at seq %d, want 5", plan.TailMessages[0].Seq)
	}
	if len(plan.SummarizeMessages) != 4 || plan.EndSeq != 4 {
		t.Fatalf("summarize mismatch: len=%d end=%d", len(plan.SummarizeMessages), plan.EndSeq)
	}
}

func TestSummarizeMessagesLocalIsNotHard320Cut(t *testing.T) {
	var messages []model.Message
	for i := 0; i < 8; i++ {
		messages = append(messages, model.Message{
			Seq:         uint64(i + 1),
			Role:        "user",
			ContentJSON: encodeMessageText(t, strings.Repeat("重要上下文 ", 40)+string(rune('A'+i))),
		})
	}
	summary := summarizeMessagesLocal(messages, 1, 8)
	if len(summary.Summary) < 400 {
		t.Fatalf("local summary too short (still hard-truncated?): %d chars", len(summary.Summary))
	}
	if !strings.Contains(summary.Summary, "local heuristic") {
		t.Fatalf("unexpected summary: %s", summary.Summary)
	}
}

func TestParseCompactSummaryJSONAcceptsFencedJSON(t *testing.T) {
	raw := "```json\n{\"summary\":\"ok\",\"decisions\":[\"use A\"],\"open_tasks\":[\"finish B\"],\"workspace_context\":[\"src/a.go\"],\"risks\":[]}\n```"
	summary, err := parseCompactSummaryJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary != "ok" || len(summary.Decisions) != 1 || summary.Decisions[0] != "use A" {
		t.Fatalf("parsed %#v", summary)
	}
}

func TestSummarizeMessagesWithLLMUsesProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{
					"content": `{"summary":"LLM summary of work","decisions":["ship it"],"open_tasks":["write tests"],"workspace_context":["main.go"],"risks":["flaky CI"]}`,
				}},
			},
		})
	}))
	defer server.Close()

	repos, service := newSessionServiceTestFixture(t)
	profile, err := repos.Providers.Create(model.ProviderProfile{
		Name:         "mock",
		Provider:     "openai_compatible",
		BaseURL:      server.URL,
		Model:        "mock-model",
		APIKeySecret: "sk-test",
		IsDefault:    true,
		Active:       true,
	})
	if err != nil {
		t.Fatal(err)
	}

	messages := []model.Message{
		{Seq: 1, Role: "user", ContentJSON: encodeMessageText(t, "implement compact")},
		{Seq: 2, Role: "assistant", ContentJSON: encodeMessageText(t, "done compact")},
	}
	summary, err := service.compactor.summarizeMessagesWithLLM(messages, 1, 2, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Summary != "LLM summary of work" || len(summary.OpenTasks) != 1 {
		t.Fatalf("summary %#v", summary)
	}
}

func TestBuildCompactSummaryFallsBackToLocalWhenNoProvider(t *testing.T) {
	_, service := newSessionServiceTestFixture(t)
	messages := []model.Message{
		{Seq: 1, Role: "user", ContentJSON: encodeMessageText(t, "hello")},
		{Seq: 2, Role: "assistant", ContentJSON: encodeMessageText(t, "world")},
	}
	summary, method, err := service.compactor.buildCompactSummary("sess", messages, 1, 2, compactSummaryOptions{Mode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if method != "local" {
		t.Fatalf("method = %q, want local", method)
	}
	if !strings.Contains(summary.Summary, "hello") {
		t.Fatalf("summary = %q", summary.Summary)
	}
}

func TestSessionServiceCompactWithTurnsKeepsLastRounds(t *testing.T) {
	repos, service := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("source", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		role string
		text string
	}{
		{"user", "turn1"},
		{"assistant", "ans1"},
		{"user", "turn2"},
		{"assistant", "ans2"},
		{"user", "turn3"},
		{"assistant", "ans3"},
	} {
		if _, err := repos.Messages.Add(source.ID, item.role, item.text, "run"); err != nil {
			t.Fatal(err)
		}
	}

	preview, err := service.CompactPreview(source.ID, CompactPreviewRequest{
		KeepTailTurns: 2,
		Mode:          "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Preview.KeepTailTurns != 2 {
		t.Fatalf("keep_tail_turns = %d", preview.Preview.KeepTailTurns)
	}
	// Summarize turn1 only (seq 1-2), keep turn2+turn3
	if preview.Preview.SourceEndSeq != 2 {
		t.Fatalf("source_end_seq = %d, want 2", preview.Preview.SourceEndSeq)
	}

	result, err := service.Compact(source.ID, CompactSessionRequest{
		KeepTailTurns: 2,
		Mode:          "local",
		Summary:       preview.Preview.Summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ID != source.ID || result.KeepTailTurns != 2 {
		t.Fatalf("compact result = %#v", result)
	}
	// Full history remains visible and unchanged in the original session.
	msgs, err := repos.Messages.List(result.Session.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 6 {
		t.Fatalf("len(messages)=%d want 6", len(msgs))
	}
	assertSessionTestMessageText(t, msgs[0], "turn1")
	assertSessionTestMessageText(t, msgs[5], "ans3")
}
