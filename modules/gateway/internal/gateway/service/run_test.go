package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/infra/runtimeclient"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

func TestRunServiceStartPassesExistingConversationToRuntime(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_context", "Context", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(session.ID, "user", "first question", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(session.ID, "assistant", "first answer", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(session.ID, "subagent", "private planner detail", "run_1"); err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	t.Setenv("RED_PANDA_RUNTIME_HELPER", "1")
	t.Setenv("RED_PANDA_RUNTIME_CAPTURE", capturePath)
	runtime := runtimeclient.New(os.Args[0], []string{"-test.run=TestRunServiceRuntimeHelperProcess"}, "test", nil, nil)
	service := NewRunService(repos, eventhub.New(), runtime)

	result, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "follow-up question"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted {
		t.Fatalf("run was not accepted: %#v", result)
	}

	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	var params methods.ReplyParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if params.Input.Text != "follow-up question" {
		t.Fatalf("input text = %q, want follow-up question", params.Input.Text)
	}
	if len(params.Session.Conversation) != 2 {
		t.Fatalf("conversation len = %d, want 2: %#v", len(params.Session.Conversation), params.Session.Conversation)
	}
	assertConversationMessage(t, params.Session.Conversation[0], "user", "first question")
	assertConversationMessage(t, params.Session.Conversation[1], "assistant", "first answer")

	rows, err := repos.Messages.List(session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("persisted messages len = %d, want 4", len(rows))
	}
	assertServiceMessage(t, rows[2], "subagent", "run_1", "private planner detail")
	assertServiceMessage(t, rows[3], "user", result.RunID, "follow-up question")
}

func TestRunServiceStartPassesLatestConversationWindowToRuntime(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_long_context", "Long context", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 205; index++ {
		if _, err := repos.Messages.Add(session.ID, "user", fmt.Sprintf("message %03d", index), "run_history"); err != nil {
			t.Fatal(err)
		}
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	t.Setenv("RED_PANDA_RUNTIME_HELPER", "1")
	t.Setenv("RED_PANDA_RUNTIME_CAPTURE", capturePath)
	runtime := runtimeclient.New(os.Args[0], []string{"-test.run=TestRunServiceRuntimeHelperProcess"}, "test", nil, nil)
	service := NewRunService(repos, eventhub.New(), runtime)

	if _, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "latest follow-up"},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	var params methods.ReplyParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Session.Conversation) != 200 {
		t.Fatalf("conversation len = %d, want 200", len(params.Session.Conversation))
	}
	assertConversationMessage(t, params.Session.Conversation[0], "user", "message 006")
	assertConversationMessage(t, params.Session.Conversation[199], "user", "message 205")
}

func TestResolveMaxConcurrentRuns(t *testing.T) {
	t.Setenv("RED_PANDA_MAX_CONCURRENT_RUNS", "")
	if got := resolveMaxConcurrentRuns(nil); got != defaultMaxConcurrentRuns {
		t.Fatalf("default = %d, want %d", got, defaultMaxConcurrentRuns)
	}
	if got := resolveMaxConcurrentRuns(map[string]any{"max_concurrent_runs": 5}); got != 5 {
		t.Fatalf("option = %d, want 5", got)
	}
	if got := resolveMaxConcurrentRuns(map[string]any{"max_concurrent_runs": 99}); got != maxConcurrentRunsCap {
		t.Fatalf("capped option = %d, want %d", got, maxConcurrentRunsCap)
	}

	t.Setenv("RED_PANDA_MAX_CONCURRENT_RUNS", "7")
	if got := resolveMaxConcurrentRuns(nil); got != 7 {
		t.Fatalf("env = %d, want 7", got)
	}
	// Per-run option still wins over env.
	if got := resolveMaxConcurrentRuns(map[string]any{"max_concurrent_runs": 2}); got != 2 {
		t.Fatalf("option over env = %d, want 2", got)
	}
}

func TestRunServiceStartRejectsSameSessionWhileActive(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_serial", "Serial", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID:        "run_active",
		SessionID: session.ID,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	t.Setenv("RED_PANDA_RUNTIME_HELPER", "1")
	t.Setenv("RED_PANDA_RUNTIME_CAPTURE", capturePath)
	runtime := runtimeclient.New(os.Args[0], []string{"-test.run=TestRunServiceRuntimeHelperProcess"}, "test", nil, nil)
	service := NewRunService(repos, eventhub.New(), runtime)

	_, err = service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "second message"},
	})
	if err == nil {
		t.Fatal("expected same-session concurrent start to fail")
	}
	if !strings.Contains(err.Error(), "会话已有任务在运行中") {
		t.Fatalf("error = %v, want session serial message", err)
	}
}

func TestRunServiceStartRejectsWhenGlobalConcurrentLimitReached(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	now := time.Now().UTC()
	for index := 1; index <= 3; index++ {
		sessionID := fmt.Sprintf("session_global_%d", index)
		if _, err := repos.Sessions.Ensure(sessionID, sessionID, "D:/workspace"); err != nil {
			t.Fatal(err)
		}
		if err := repos.Runs.Start(model.RunRecord{
			ID:        fmt.Sprintf("run_global_%d", index),
			SessionID: sessionID,
			Status:    "running",
			StartedAt: now.Add(time.Duration(index) * time.Millisecond),
		}); err != nil {
			t.Fatal(err)
		}
	}

	session, err := repos.Sessions.Ensure("session_global_new", "New", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	t.Setenv("RED_PANDA_RUNTIME_HELPER", "1")
	t.Setenv("RED_PANDA_RUNTIME_CAPTURE", capturePath)
	t.Setenv("RED_PANDA_MAX_CONCURRENT_RUNS", "")
	runtime := runtimeclient.New(os.Args[0], []string{"-test.run=TestRunServiceRuntimeHelperProcess"}, "test", nil, nil)
	service := NewRunService(repos, eventhub.New(), runtime)

	_, err = service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "should be rejected"},
		Options:   map[string]any{"max_concurrent_runs": 3},
	})
	if err == nil {
		t.Fatal("expected global concurrent limit rejection")
	}
	if !strings.Contains(err.Error(), "已达到最大并发运行数") {
		t.Fatalf("error = %v, want concurrent limit message", err)
	}
}

func TestRunServiceRuntimeStatusIncludesActiveRuns(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	if err := repos.Runs.Start(model.RunRecord{
		ID:        "run_status_1",
		SessionID: "session_status",
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	status := service.RuntimeStatus()
	if status["max_concurrent_runs"] != defaultMaxConcurrentRuns {
		t.Fatalf("max_concurrent_runs = %#v, want %d", status["max_concurrent_runs"], defaultMaxConcurrentRuns)
	}
	if status["active_runs"] != int64(1) {
		t.Fatalf("active_runs = %#v, want 1", status["active_runs"])
	}
}

func TestRunServiceRuntimeHelperProcess(t *testing.T) {
	if os.Getenv("RED_PANDA_RUNTIME_HELPER") != "1" {
		return
	}

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request jsonrpc.Request
		if err := decoder.Decode(&request); err != nil {
			return
		}
		switch request.Method {
		case methods.CoreInitialize:
			response, err := jsonrpc.NewResult(request.ID, methods.InitializeResult{
				ProtocolVersion: events.ProtocolVersion,
				Server:          methods.PeerInfo{Name: "test-runtime", Version: "test"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
		case methods.AgentReply:
			if err := os.WriteFile(os.Getenv("RED_PANDA_RUNTIME_CAPTURE"), request.Params, 0o600); err != nil {
				t.Fatal(err)
			}
			var params methods.ReplyParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			response, err := jsonrpc.NewResult(request.ID, methods.ReplyAccepted{
				Accepted: true,
				RunID:    params.RunID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
			return
		default:
			if err := encoder.Encode(jsonrpc.NewError(request.ID, -32601, "method not found")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRunServiceProjectsPermissionRequired(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "service.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}

	repos := repository.NewSet(db)
	service := NewRunService(repos, eventhub.New(), nil)
	service.HandleRuntimeEvent(events.Envelope{
		EventID:   "evt_perm",
		RootRunID: "run_1",
		RunID:     "run_1",
		SessionID: "session_1",
		RootSeq:   1,
		AgentSeq:  1,
		Agent: events.AgentRef{
			AgentID: "root",
			Role:    events.AgentRoleRoot,
			Path:    []string{"root"},
		},
		Type: events.EventPermissionRequest,
		Payload: map[string]any{
			"permission_id": "perm_1",
			"run_id":        "run_1",
			"tool_call_id":  "tool_1",
			"tool_name":     "shell.exec",
			"risk":          "high",
			"summary":       "Allow shell",
		},
		CreatedAt: time.Now().UTC(),
	})

	pending, err := repos.Permissions.ListPending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "perm_1" {
		t.Fatalf("permission projection mismatch: %#v", pending)
	}
}

func TestRunServiceAggregatesMessageDeltasByRunAndRole(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	if _, err := repos.Messages.Add("session_1", "user", "prompt", "run_1"); err != nil {
		t.Fatal(err)
	}

	service.HandleRuntimeEvent(messageDeltaEvent("evt_1", "run_1", "session_1", 1, events.AgentRoleRoot, "hello "))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_2", "run_1", "session_1", 2, events.AgentRoleRoot, "world"))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_3", "run_2", "session_1", 1, events.AgentRoleRoot, "new run"))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_4", "run_2", "session_1", 2, events.AgentRoleSubAgent, "plan "))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_5", "run_2", "session_1", 3, events.AgentRoleSubAgent, "step"))

	rows, err := repos.Messages.List("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	assertServiceMessage(t, rows[0], "user", "run_1", "prompt")
	assertServiceMessage(t, rows[1], "assistant", "run_1", "hello world")
	assertServiceMessage(t, rows[2], "assistant", "run_2", "new run")
	assertServiceMessage(t, rows[3], "subagent", "run_2", "plan step")
}

func TestRunServiceApplyProviderProfile(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	profile, err := repos.Providers.Create(model.ProviderProfile{
		Name:         "openai",
		Provider:     "openai_compatible",
		BaseURL:      "https://provider.invalid/v1",
		Model:        "profile-model",
		APIKeySecret: "sk-profile",
		IsDefault:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	params := methods.ReplyParams{
		Options: methods.ReplyOptions{
			ProviderProfileID: profile.ID,
		},
	}
	if err := service.applyProviderProfile(&params); err != nil {
		t.Fatal(err)
	}
	if params.Options.ProviderName != "openai_compatible" ||
		params.Options.ProviderBaseURL != "https://provider.invalid/v1" ||
		params.Options.ProviderAPIKey != "sk-profile" ||
		params.Options.Model != "profile-model" {
		t.Fatalf("provider profile options mismatch: %#v", params.Options)
	}

	params.Options.Model = "explicit-model"
	if err := service.applyProviderProfile(&params); err != nil {
		t.Fatal(err)
	}
	if params.Options.Model != "explicit-model" {
		t.Fatalf("explicit run model should win, got %q", params.Options.Model)
	}
}

func TestRunServiceApplyMemoryContext(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	if _, err := repos.Memory.Create(model.MemoryRecord{
		Scope:         "project",
		Kind:          "fact",
		Status:        "active",
		Title:         "Project memory",
		Content:       "Use project memory.",
		Confidence:    "high",
		WorkspaceRoot: "D:/workspace",
		Source:        "user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Memory.Create(model.MemoryRecord{
		Scope:      "session",
		Kind:       "decision",
		Status:     "active",
		Title:      "Session memory",
		Content:    "Use session memory.",
		Confidence: "medium",
		SessionID:  "session_memory",
		Source:     "user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Memory.Create(model.MemoryRecord{
		Scope:         "project",
		Kind:          "fact",
		Status:        "active",
		Title:         "Other project",
		Content:       "Do not use other project memory.",
		Confidence:    "high",
		WorkspaceRoot: "D:/other",
		Source:        "user",
	}); err != nil {
		t.Fatal(err)
	}

	params := methods.ReplyParams{
		Session: methods.ReplySession{
			ID:         "session_memory",
			WorkingDir: "D:/workspace",
		},
		Input: methods.ReplyInput{Text: "hello"},
	}
	if err := service.applyMemoryContext(&params); err != nil {
		t.Fatal(err)
	}
	if params.Options.MemoryContext == nil {
		t.Fatal("expected memory context")
	}
	if len(params.Options.MemoryContext.Items) != 2 {
		t.Fatalf("memory items len = %d, want 2: %#v", len(params.Options.MemoryContext.Items), params.Options.MemoryContext.Items)
	}
	if params.Input.Text != "hello" {
		t.Fatalf("input text was mutated: %q", params.Input.Text)
	}
	if params.Options.MemoryContext.Items[0].Scope != "session" || params.Options.MemoryContext.Items[1].Scope != "project" {
		t.Fatalf("memory item order mismatch: %#v", params.Options.MemoryContext.Items)
	}
	if params.Options.MemoryContext.Context == "" ||
		!containsText(params.Options.MemoryContext.Context, "Use project memory.") ||
		!containsText(params.Options.MemoryContext.Context, "Use session memory.") ||
		containsText(params.Options.MemoryContext.Context, "Do not use other project memory.") {
		t.Fatalf("memory context mismatch: %q", params.Options.MemoryContext.Context)
	}
}

func TestRunServicePersistsMemoryInjectedEvent(t *testing.T) {
	_, service := newRunServiceTestFixture(t)
	service.HandleRuntimeEvent(events.Envelope{
		EventID:   "evt_memory",
		RootRunID: "run_memory",
		RunID:     "run_memory",
		SessionID: "session_memory",
		RootSeq:   1,
		AgentSeq:  1,
		Agent: events.AgentRef{
			AgentID: "root",
			Role:    events.AgentRoleRoot,
			Path:    []string{"root"},
		},
		Type: events.EventMemoryInjected,
		Payload: map[string]any{
			"memory_ids": []any{"mem_1"},
			"count":      1,
		},
		CreatedAt: time.Now().UTC(),
	})

	items, err := service.Events("run_memory", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != string(events.EventMemoryInjected) || items[0].Payload["count"] != float64(1) {
		t.Fatalf("memory event projection mismatch: %#v", items)
	}
}

func TestRunServiceEventsReturnsEventTimeline(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	now := time.Now().UTC()
	if err := repos.RunEvents.Save(events.Envelope{
		EventID:   "evt_1",
		RootRunID: "run_events",
		RunID:     "run_events",
		SessionID: "session_1",
		RootSeq:   1,
		AgentSeq:  1,
		Agent: events.AgentRef{
			AgentID: "root",
			Role:    events.AgentRoleRoot,
			Name:    "root",
			Path:    []string{"root"},
		},
		Stream: &events.StreamRef{
			StreamID: "stream_1",
			Kind:     events.StreamMessage,
			Seq:      1,
		},
		Type:      events.EventMessageDelta,
		Payload:   map[string]any{"delta": "hello"},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	items, err := service.Events("run_events", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != "evt_1" || items[0].RootSeq != 1 || items[0].AgentRole != "root" || items[0].StreamKind != "message" {
		t.Fatalf("event dto mismatch: %#v", items[0])
	}
	if items[0].Payload["delta"] != "hello" {
		t.Fatalf("payload mismatch: %#v", items[0].Payload)
	}
}

func containsText(value string, needle string) bool {
	return strings.Contains(value, needle)
}

func newRunServiceTestFixture(t *testing.T) (repository.Set, RunService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "service.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}

	repos := repository.NewSet(db)
	return repos, NewRunService(repos, eventhub.New(), nil)
}

func messageDeltaEvent(eventID string, runID string, sessionID string, rootSeq uint64, role events.AgentRole, delta string) events.Envelope {
	agentID := "root"
	path := []string{"root"}
	if role == events.AgentRoleSubAgent {
		agentID = "subagent_1"
		path = []string{"root", "subagent_1"}
	}
	return events.Envelope{
		EventID:   eventID,
		RootRunID: runID,
		RunID:     runID,
		SessionID: sessionID,
		RootSeq:   rootSeq,
		AgentSeq:  rootSeq,
		Agent: events.AgentRef{
			AgentID: agentID,
			Role:    role,
			Path:    path,
		},
		Type:      events.EventMessageDelta,
		Payload:   map[string]any{"delta": delta},
		CreatedAt: time.Now().UTC(),
	}
}

func assertServiceMessage(t *testing.T, row model.Message, role string, runID string, text string) {
	t.Helper()
	if row.Role != role || row.RunID != runID {
		t.Fatalf("message metadata = role:%s run:%s, want role:%s run:%s", row.Role, row.RunID, role, runID)
	}
	var content []methods.ContentBlock
	if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Text != text {
		t.Fatalf("message text = %#v, want %q", content, text)
	}
}

func assertConversationMessage(t *testing.T, message methods.Message, role string, text string) {
	t.Helper()
	if message.Role != role {
		t.Fatalf("conversation role = %q, want %q", message.Role, role)
	}
	if len(message.Content) != 1 || message.Content[0].Text != text {
		t.Fatalf("conversation content = %#v, want %q", message.Content, text)
	}
	if message.ID == "" || message.CreatedAt == "" {
		t.Fatalf("conversation metadata is incomplete: %#v", message)
	}
}
