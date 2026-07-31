package runtime

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestPromptComposerPreservesPolicyAndContextOrder(t *testing.T) {
	fixed := time.Date(2026, time.July, 15, 9, 8, 7, 0, time.FixedZone("CST", 8*60*60))
	stream := false
	composer := promptComposer{now: func() time.Time { return fixed }}
	params := methods.ReplyParams{
		RunID: "run_1",
		Session: methods.ReplySession{ID: "session_1", Conversation: []methods.Message{
			{Role: "user", Content: []methods.ContentBlock{{Type: "text", Text: "first"}, {Type: "text", Text: "detail"}}},
			{Role: "assistant", Content: []methods.ContentBlock{{Type: "text", Text: "answer"}}},
			{Role: "worker", Content: []methods.ContentBlock{{Type: "text", Text: "ignored"}}},
		}},
		Options: methods.ReplyOptions{
			ProviderName: "openai_compatible", ProviderBaseURL: "https://example.test/v1", ProviderAPIKey: "secret", ProviderStream: &stream, Model: "model-a", ReasoningEffort: "high", LogLLMRequests: true,
			SpecialistContext: &methods.SpecialistContext{Context: " specialist "},
			MemoryContext:     &methods.MemoryContext{Context: " memory "},
			TodoContext:       &methods.TodoContext{Context: " todo context "},
			SkillsContext:     &methods.SkillsContext{Context: " skills "},
		},
	}
	definitions := []tools.Definition{{Name: "worker.delegate"}, {Name: "todo.write"}}
	rounds := [][]provider.ToolExchange{{{
		Call:   tools.Call{ID: "call_1", Name: "worker.delegate"},
		Result: tools.Result{ToolCallID: "call_1", Name: "worker.delegate", Status: tools.CallStatusCompleted, Output: "report"},
	}}}

	request := composer.compose(params, "current", definitions, rounds)
	messages := request.Prompt.FlattenMessages()
	if request.RunID != "run_1" || request.SessionID != "session_1" || request.Input != "current" {
		t.Fatalf("identity/input changed: %#v", request)
	}
	wantOptions := provider.RequestOptions{ProviderName: "openai_compatible", ProviderBaseURL: "https://example.test/v1", ProviderAPIKey: "secret", Stream: &stream, Model: "model-a", ReasoningEffort: "high", LogLLMRequests: true}
	if !reflect.DeepEqual(request.Options, wantOptions) {
		t.Fatalf("options = %#v, want %#v", request.Options, wantOptions)
	}
	wantContents := []string{
		rootAgentOrchestrationPolicy,
		rootAgentPostDelegationPolicy,
		rootAgentTodoPolicy,
		"specialist", "skills",
		"first\ndetail", "answer",
		"memory\n\n[Runtime todo context]\ntodo context\n[/Runtime todo context]\n\ncurrent",
	}
	if len(messages) != len(wantContents) {
		t.Fatalf("messages = %#v", messages)
	}
	for i, want := range wantContents {
		got := provider.MessageText(messages[i].Content)
		if got != want {
			t.Fatalf("messages[%d].Content = %q, want %q", i, got, want)
		}
	}
	if got := []string{messages[5].Role, messages[6].Role, messages[7].Role}; !reflect.DeepEqual(got, []string{"user", "assistant", "user"}) {
		t.Fatalf("conversation roles = %#v", got)
	}
	if len(request.Prompt.StablePrefix) != 3 || len(request.Prompt.SessionPrefix) != 2 || len(request.Prompt.History) != 2 || len(request.Prompt.TurnTail) != 1 {
		t.Fatalf("prompt segments = %#v", request.Prompt)
	}
	if !reflect.DeepEqual(request.ToolRounds, rounds) || len(request.ToolHistory) != 1 {
		t.Fatalf("tool history changed: %#v", request)
	}
}

func TestPromptComposerAddsFileChangeReportPolicyOnlyToRoot(t *testing.T) {
	composer := promptComposer{now: time.Now}
	definitions := []tools.Definition{{Name: "git.status"}, {Name: "workspace.write_file"}}
	root := composer.compose(methods.ReplyParams{}, "edit", definitions, nil)
	rootMessages := root.Prompt.FlattenMessages()
	rootPolicy := provider.MessageText(rootMessages[0].Content)
	if !strings.Contains(rootPolicy, "变更文件") || !strings.Contains(rootPolicy, "Markdown link") {
		t.Fatalf("root file report policy missing: %#v", rootMessages)
	}

	specialist := composer.compose(methods.ReplyParams{Options: methods.ReplyOptions{
		SpecialistContext: &methods.SpecialistContext{Context: "specialist"},
	}}, "edit", definitions, nil)
	for _, message := range specialist.Prompt.FlattenMessages() {
		if strings.Contains(provider.MessageText(message.Content), "变更文件") {
			t.Fatalf("specialist received root report policy: %#v", specialist.Prompt.FlattenMessages())
		}
	}
}

func TestFormatUTCOffset(t *testing.T) {
	for _, tt := range []struct {
		seconds int
		want    string
	}{{8 * 3600, "+08:00"}, {-5*3600 - 30*60, "-05:30"}, {0, "+00:00"}} {
		if got := formatUTCOffset(tt.seconds); got != tt.want {
			t.Fatalf("formatUTCOffset(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestPromptComposerOnlyInjectsTimeForTimeSensitiveRequests(t *testing.T) {
	first := promptComposer{now: func() time.Time { return time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC) }}
	second := promptComposer{now: func() time.Time { return time.Date(2026, 7, 15, 9, 0, 30, 0, time.UTC) }}
	ordinaryA := first.compose(methods.ReplyParams{}, "fix the parser", nil, nil)
	ordinaryB := second.compose(methods.ReplyParams{}, "fix the parser", nil, nil)
	if !reflect.DeepEqual(ordinaryA.Prompt, ordinaryB.Prompt) {
		t.Fatalf("ordinary request changed with wall clock: %#v != %#v", ordinaryA.Prompt, ordinaryB.Prompt)
	}
	if shouldInjectCurrentTime("update the database") {
		t.Fatal("update/database must not be treated as the word date")
	}
	timeRequest := first.compose(methods.ReplyParams{}, "现在几点？", nil, nil)
	timeMessages := timeRequest.Prompt.FlattenMessages()
	if got := provider.MessageText(timeMessages[len(timeMessages)-1].Content); !strings.Contains(got, "2026-07-15 09:00:00") {
		t.Fatalf("time context missing: %q", got)
	}
}

func TestPromptComposerKeepsCacheableSegmentsStableAcrossDynamicTurns(t *testing.T) {
	composer := promptComposer{now: func() time.Time { return time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC) }}
	base := methods.ReplyParams{Options: methods.ReplyOptions{
		ProviderName:      "openai_compatible",
		Model:             "model-a",
		SpecialistContext: &methods.SpecialistContext{Context: "specialist-v1"},
		SkillsContext:     &methods.SkillsContext{Context: "skills-v1"},
		MemoryContext:     &methods.MemoryContext{Context: "memory-a"},
		TodoContext:       &methods.TodoContext{Context: "todo-a"},
	}}
	definitions := []tools.Definition{{Name: "todo.write"}, {Name: "worker.delegate"}}
	first := composer.compose(base, "first question", definitions, nil)

	changed := base
	changed.Options.MemoryContext = &methods.MemoryContext{Context: "memory-b"}
	changed.Options.TodoContext = &methods.TodoContext{Context: "todo-b"}
	second := composer.compose(changed, "second question", definitions, nil)

	if !reflect.DeepEqual(first.Prompt.StablePrefix, second.Prompt.StablePrefix) {
		t.Fatalf("stable prefix changed: %#v != %#v", first.Prompt.StablePrefix, second.Prompt.StablePrefix)
	}
	if !reflect.DeepEqual(first.Prompt.SessionPrefix, second.Prompt.SessionPrefix) {
		t.Fatalf("session prefix changed: %#v != %#v", first.Prompt.SessionPrefix, second.Prompt.SessionPrefix)
	}
	if first.Prompt.CacheEpoch != second.Prompt.CacheEpoch {
		t.Fatalf("dynamic turn changed cache epoch: %q != %q", first.Prompt.CacheEpoch, second.Prompt.CacheEpoch)
	}
	if reflect.DeepEqual(first.Prompt.TurnTail, second.Prompt.TurnTail) {
		t.Fatalf("turn tail did not capture dynamic context: %#v", first.Prompt.TurnTail)
	}
}

func TestPromptComposerPromotesCompactionSummaryToSessionPrefix(t *testing.T) {
	params := methods.ReplyParams{
		Options: methods.ReplyOptions{ProviderName: "openai_compatible", Model: "m"},
		Session: methods.ReplySession{Conversation: []methods.Message{
			{Role: "system", Content: []methods.ContentBlock{{Type: "text", Text: "summary-v1"}}},
			{Role: "user", Content: []methods.ContentBlock{{Type: "text", Text: "recent question"}}},
			{Role: "assistant", Content: []methods.ContentBlock{{Type: "text", Text: "recent answer"}}},
		}},
	}
	first := newPromptComposer().compose(params, "current", nil, nil)
	if len(first.Prompt.SessionPrefix) != 1 || provider.MessageText(first.Prompt.SessionPrefix[0].Content) != "summary-v1" {
		t.Fatalf("session prefix = %#v", first.Prompt.SessionPrefix)
	}
	if len(first.Prompt.History) != 2 {
		t.Fatalf("history = %#v", first.Prompt.History)
	}

	params.Session.Conversation[0].Content[0].Text = "summary-v2"
	second := newPromptComposer().compose(params, "current", nil, nil)
	if first.Prompt.CacheEpoch == second.Prompt.CacheEpoch {
		t.Fatal("compaction summary change did not invalidate cache epoch")
	}
}

func TestPromptComposerMultimodalAttachments(t *testing.T) {
	composer := promptComposer{now: time.Now}
	params := methods.ReplyParams{
		RunID: "run_img",
		Session: methods.ReplySession{ID: "session_img", Conversation: []methods.Message{
			{Role: "user", Content: []methods.ContentBlock{
				{Type: "text", Text: "older"},
				{Type: "image_ref", AttachmentID: "att_old", MIME: "image/png", Alt: "old.png", Width: 10, Height: 10},
			}},
			{Role: "assistant", Content: []methods.ContentBlock{{Type: "text", Text: "saw it"}}},
		}},
		Input: methods.ReplyInput{
			Text: "what color?",
			Attachments: []methods.InputAttachment{{
				AttachmentID: "att_new",
				MIME:         "image/png",
				DataB64:      "QQ==",
				ByteSize:     1,
			}},
		},
	}
	request := composer.compose(params, "what color?", nil, nil)
	messages := request.Prompt.FlattenMessages()
	if len(request.Attachments) != 1 || request.Attachments[0].ID != "att_new" {
		t.Fatalf("Attachments = %#v", request.Attachments)
	}
	// Last message is the current user turn with multimodal parts.
	last := messages[len(messages)-1]
	parts, ok := last.Content.([]provider.Part)
	if !ok || len(parts) != 2 {
		t.Fatalf("current user content = %#v", last.Content)
	}
	if parts[0].Type != "text" || parts[0].Text != "what color?" {
		t.Fatalf("text part = %#v", parts[0])
	}
	if parts[1].ImageURL == nil || !strings.Contains(parts[1].ImageURL.URL, "base64,QQ==") {
		t.Fatalf("image part = %#v", parts[1])
	}
	// History image_ref without inline bytes becomes a text placeholder.
	var historyUser provider.Message
	foundHistory := false
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		// Skip the trailing current-turn message.
		if m.Content == nil {
			// fall through to identity by text placeholder presence
		}
		parts, ok := m.Content.([]provider.Part)
		if !ok {
			continue
		}
		hasPlaceholder := false
		for _, p := range parts {
			if strings.Contains(p.Text, "[image:") {
				hasPlaceholder = true
				break
			}
		}
		if hasPlaceholder {
			historyUser = m
			foundHistory = true
			break
		}
	}
	if !foundHistory {
		t.Fatalf("history user with image placeholder not found: %#v", messages)
	}
	hParts, ok := historyUser.Content.([]provider.Part)
	if !ok {
		t.Fatalf("history user content should be []Part, got %#v", historyUser.Content)
	}
	foundPlaceholder := false
	for _, p := range hParts {
		if strings.Contains(p.Text, "[image:") {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Fatalf("expected image placeholder in history, got %#v", hParts)
	}
}

func TestConversationMessageTextKeepsImagePlaceholder(t *testing.T) {
	got := conversationMessageText(methods.Message{Content: []methods.ContentBlock{
		{Type: "text", Text: "see"},
		{Type: "image_ref", AttachmentID: "att_1", Alt: "shot.png"},
	}})
	if got != "see\n[image: shot.png]" {
		t.Fatalf("got %q", got)
	}
}
