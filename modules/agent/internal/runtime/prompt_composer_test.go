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
	if request.RunID != "run_1" || request.SessionID != "session_1" || request.Input != "current" {
		t.Fatalf("identity/input changed: %#v", request)
	}
	wantOptions := provider.RequestOptions{ProviderName: "openai_compatible", ProviderBaseURL: "https://example.test/v1", ProviderAPIKey: "secret", Stream: &stream, Model: "model-a", ReasoningEffort: "high", LogLLMRequests: true}
	if !reflect.DeepEqual(request.Options, wantOptions) {
		t.Fatalf("options = %#v, want %#v", request.Options, wantOptions)
	}
	wantContents := []string{
		currentTimeContextMessage(fixed),
		rootAgentOrchestrationPolicy,
		rootAgentPostDelegationPolicy,
		rootAgentTodoPolicy,
		"specialist", "memory", "todo context", "skills",
		"first\ndetail", "answer", "current",
	}
	if len(request.Messages) != len(wantContents) {
		t.Fatalf("messages = %#v", request.Messages)
	}
	for i, want := range wantContents {
		got := provider.MessageText(request.Messages[i].Content)
		if got != want {
			t.Fatalf("messages[%d].Content = %q, want %q", i, got, want)
		}
	}
	if got := []string{request.Messages[8].Role, request.Messages[9].Role, request.Messages[10].Role}; !reflect.DeepEqual(got, []string{"user", "assistant", "user"}) {
		t.Fatalf("conversation roles = %#v", got)
	}
	if !reflect.DeepEqual(request.ToolRounds, rounds) || len(request.ToolHistory) != 1 {
		t.Fatalf("tool history changed: %#v", request)
	}
}

func TestPromptComposerAddsFileChangeReportPolicyOnlyToRoot(t *testing.T) {
	composer := promptComposer{now: time.Now}
	definitions := []tools.Definition{{Name: "git.status"}, {Name: "workspace.write_file"}}
	root := composer.compose(methods.ReplyParams{}, "edit", definitions, nil)
	rootPolicy := provider.MessageText(root.Messages[1].Content)
	if !strings.Contains(rootPolicy, "变更文件") || !strings.Contains(rootPolicy, "Markdown link") {
		t.Fatalf("root file report policy missing: %#v", root.Messages)
	}

	specialist := composer.compose(methods.ReplyParams{Options: methods.ReplyOptions{
		SpecialistContext: &methods.SpecialistContext{Context: "specialist"},
	}}, "edit", definitions, nil)
	for _, message := range specialist.Messages {
		if strings.Contains(provider.MessageText(message.Content), "变更文件") {
			t.Fatalf("specialist received root report policy: %#v", specialist.Messages)
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
	if len(request.Attachments) != 1 || request.Attachments[0].ID != "att_new" {
		t.Fatalf("Attachments = %#v", request.Attachments)
	}
	// Last message is the current user turn with multimodal parts.
	last := request.Messages[len(request.Messages)-1]
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
	for _, m := range request.Messages {
		if m.Role != "user" {
			continue
		}
		// Skip the trailing current-turn message.
		if &m == &request.Messages[len(request.Messages)-1] || m.Content == nil {
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
		t.Fatalf("history user with image placeholder not found: %#v", request.Messages)
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
