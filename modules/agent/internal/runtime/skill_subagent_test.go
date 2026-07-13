package runtime

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type skillRunProvider struct {
	mu       sync.Mutex
	requests []ProviderRequest
}

func (*skillRunProvider) Name() string {
	return "skill-run-test"
}

func (p *skillRunProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	requestCount := len(p.requests)
	p.mu.Unlock()
	if requestCount == 1 {
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:   "call_skill_run",
			Name: "skill.run",
			Arguments: map[string]any{
				"name": "review",
				"task": "inspect the current change",
			},
		}}})
	}
	if err := emit(ProviderChunk{Delta: "ROOT_SKILL_DONE"}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

type recordingSkillProcess struct {
	mu          sync.Mutex
	childParams methods.ReplyParams
	closeCount  int
}

func (p *recordingSkillProcess) Start(_ context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error {
	p.mu.Lock()
	p.childParams = childParams
	p.mu.Unlock()
	onEvent(skillChildEvent(childParams, events.EventReasoningDelta, "PRIVATE_REASONING", false))
	onEvent(skillChildEvent(childParams, events.EventMessageDelta, "SKILL_", false))
	onEvent(skillChildEvent(childParams, events.EventMessageDelta, "PUBLIC_RESULT", true))
	onEvent(events.Envelope{
		RootRunID: childParams.RunID,
		RunID:     childParams.RunID,
		SessionID: childParams.Session.ID,
		Agent:     events.AgentRef{AgentID: "root", Role: events.AgentRoleRoot, Path: []string{"root"}},
		Type:      events.EventFinish,
		Payload:   map[string]any{"status": "completed"},
	})
	return nil
}

func (*recordingSkillProcess) Cancel(context.Context, string, string) error {
	return nil
}

func (p *recordingSkillProcess) Close(context.Context) error {
	p.mu.Lock()
	p.closeCount++
	p.mu.Unlock()
	return nil
}

func TestRuntimeRunsSkillInIsolatedProcessSubAgent(t *testing.T) {
	root := t.TempDir()
	if _, err := runCreateSkill(root, "review", "Private review skill.", "SKILL_PRIVATE_SENTINEL"); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	lines := make(chan []byte, 64)
	go readJSONLines(t, reader, lines)

	provider := &skillRunProvider{}
	process := &recordingSkillProcess{}
	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider
	rt.newProcessSubAgent = func(context.Context, methods.ReplyParams, string) (processSubAgent, error) {
		return process, nil
	}
	sendRequest(t, context.Background(), rt, "reply_skill_run", methods.AgentReply, methods.ReplyParams{
		RunID: "run_skill_isolation",
		Session: methods.ReplySession{
			ID:         "session_skill_isolation",
			WorkingDir: root,
			Conversation: []methods.Message{{
				Role:    "user",
				Content: []methods.ContentBlock{{Type: "text", Text: "ROOT_HISTORY_SENTINEL"}},
			}},
		},
		Input: methods.ReplyInput{Text: "run the review skill"},
		Options: methods.ReplyOptions{
			ToolPolicy:     "allow_all",
			PermissionMode: "strict",
			MemoryContext: &methods.MemoryContext{
				Context: "ROOT_MEMORY_SENTINEL",
			},
		},
	})
	waitForResponse(t, lines, "reply_skill_run")
	runEvents := waitForEventsUntilFinish(t, lines)
	assertEventSequence(t, runEvents, []events.EventType{
		events.EventToolStarted,
		events.EventSubAgentUpdate,
		events.EventReasoningDelta,
		events.EventMessageDelta,
		events.EventSubAgentUpdate,
		events.EventToolOutput,
		events.EventToolFinished,
		events.EventMessageDelta,
		events.EventFinish,
	})

	provider.mu.Lock()
	requests := append([]ProviderRequest(nil), provider.requests...)
	provider.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider request count = %d, want 2", len(requests))
	}
	for index, request := range requests {
		if len(request.Session.Conversation) != 1 || conversationMessageText(request.Session.Conversation[0]) != "ROOT_HISTORY_SENTINEL" {
			t.Fatalf("root request %d conversation changed: %#v", index+1, request.Session.Conversation)
		}
		if strings.Contains(request.Input.Text, "SKILL_PRIVATE_SENTINEL") {
			t.Fatalf("root request %d leaked private skill instructions", index+1)
		}
	}
	if len(requests[1].ToolHistory) != 1 {
		t.Fatalf("root tool history = %#v", requests[1].ToolHistory)
	}
	skillOut := requests[1].ToolHistory[0].Result.Output
	if !strings.Contains(skillOut, "SKILL_PUBLIC_RESULT") {
		t.Fatalf("root tool history output missing skill result: %q", skillOut)
	}

	process.mu.Lock()
	childParams := process.childParams
	closeCount := process.closeCount
	process.mu.Unlock()
	if childParams.RunID == "run_skill_isolation" || childParams.Session.ID != "session_skill_isolation" {
		t.Fatalf("child identity = %#v", childParams)
	}
	if len(childParams.Session.Conversation) != 0 {
		t.Fatalf("child inherited root conversation: %#v", childParams.Session.Conversation)
	}
	if strings.Contains(childParams.Input.Text, "SKILL_PRIVATE_SENTINEL") || !strings.Contains(childParams.Input.Text, "inspect the current change") {
		t.Fatalf("child task input is not isolated from skill instructions: %q", childParams.Input.Text)
	}
	if childParams.Options.MemoryContext != nil {
		t.Fatalf("skill child must not put skill body in MemoryContext: %#v", childParams.Options.MemoryContext)
	}
	if childParams.Options.SpecialistContext == nil || !strings.Contains(childParams.Options.SpecialistContext.Context, "SKILL_PRIVATE_SENTINEL") {
		t.Fatalf("child system skill context is missing: %#v", childParams.Options.SpecialistContext)
	}
	if childParams.Options.SpecialistContext.Kind != "skill" {
		t.Fatalf("specialist kind = %q, want skill", childParams.Options.SpecialistContext.Kind)
	}
	if childParams.Options.SpawnSubAgents || childParams.Options.SubAgentBackend != "" {
		t.Fatalf("child options are not isolated: %#v", childParams.Options)
	}
	for _, denied := range skillSubagentDenylist {
		if !containsString(childParams.Options.ToolDenylist, denied) {
			t.Fatalf("child denylist missing %s: %#v", denied, childParams.Options.ToolDenylist)
		}
	}
	if closeCount != 1 {
		t.Fatalf("child close count = %d, want 1", closeCount)
	}
	finish := runEvents[len(runEvents)-1]
	if finish.Payload["status"] != "completed" {
		t.Fatalf("root finish = %#v", finish.Payload)
	}
}

func skillChildEvent(params methods.ReplyParams, typ events.EventType, delta string, final bool) events.Envelope {
	kind := events.StreamMessage
	if typ == events.EventReasoningDelta {
		kind = events.StreamReasoning
	}
	return events.Envelope{
		RootRunID: params.RunID,
		RunID:     params.RunID,
		SessionID: params.Session.ID,
		Agent:     events.AgentRef{AgentID: "root", Role: events.AgentRoleRoot, Path: []string{"root"}},
		Stream:    &events.StreamRef{StreamID: "skill_stream", Kind: kind, Seq: 1, Final: final},
		Type:      typ,
		Payload:   map[string]any{"delta": delta},
	}
}
