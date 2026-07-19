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
		if request.Messages[i].Content != want {
			t.Fatalf("messages[%d].Content = %q, want %q", i, request.Messages[i].Content, want)
		}
	}
	if got := []string{request.Messages[8].Role, request.Messages[9].Role, request.Messages[10].Role}; !reflect.DeepEqual(got, []string{"user", "assistant", "user"}) {
		t.Fatalf("conversation roles = %#v", got)
	}
	if !reflect.DeepEqual(request.ToolRounds, rounds) || len(request.ToolHistory) != 1 {
		t.Fatalf("tool history changed: %#v", request)
	}
}

func TestPromptComposerGoalControllerSuppressesWorkerAndTodoPolicies(t *testing.T) {
	params := methods.ReplyParams{Options: methods.ReplyOptions{GoalContext: &methods.GoalContext{GoalID: "goal_1", Context: "goal context"}}}
	request := (promptComposer{now: time.Now}).compose(params, "continue", []tools.Definition{
		{Name: "worker.delegate"}, {Name: "todo.write"}, {Name: "goal.create"},
	}, nil)
	joined := make([]string, 0, len(request.Messages))
	for _, message := range request.Messages {
		joined = append(joined, message.Content)
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(all, "largest evidence-backed gap") || !strings.Contains(all, "id=goal_1") {
		t.Fatalf("goal policy/binding missing: %s", all)
	}
	if strings.Contains(all, "spawn multiple worker.delegate calls IN ONE TURN") || strings.Contains(all, "Session task list") {
		t.Fatalf("goal controller received worker/todo policy: %s", all)
	}
}

func TestPromptComposerAddsFileChangeReportPolicyOnlyToRoot(t *testing.T) {
	composer := promptComposer{now: time.Now}
	definitions := []tools.Definition{{Name: "git.status"}, {Name: "workspace.write_file"}}
	root := composer.compose(methods.ReplyParams{}, "edit", definitions, nil)
	if !strings.Contains(root.Messages[1].Content, "变更文件") || !strings.Contains(root.Messages[1].Content, "Markdown link") {
		t.Fatalf("root file report policy missing: %#v", root.Messages)
	}

	specialist := composer.compose(methods.ReplyParams{Options: methods.ReplyOptions{
		SpecialistContext: &methods.SpecialistContext{Context: "specialist"},
	}}, "edit", definitions, nil)
	for _, message := range specialist.Messages {
		if strings.Contains(message.Content, "变更文件") {
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
