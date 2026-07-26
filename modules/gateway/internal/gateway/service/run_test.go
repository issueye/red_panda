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
	runtime := useStdioRuntimeHelper(t, capturePath)
	service := NewRunService(repos, eventhub.New(), runtime)

	result, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "follow-up question"},
		Options: map[string]any{
			"spawn_subagents":  true,
			"subagent_backend": "runtime_process",
		},
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
	var params methods.RunExecuteParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "spawn_subagents") || strings.Contains(string(raw), "subagent_backend") ||
		strings.Contains(string(raw), "agent_definitions") {
		t.Fatalf("v0.2 run request contains legacy agent fields: %s", raw)
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

func TestRunServiceStartFinishesAdmittedRunWhenPreparationFails(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_prepare_failure", "Prepare failure", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}

	runtime := useStdioRuntimeHelper(t, filepath.Join(t.TempDir(), "unused.json"))
	service := NewRunService(repos, eventhub.New(), runtime)
	_, err = service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "will fail before dispatch"},
		Options:   map[string]any{"provider_profile_id": "missing_profile"},
	})
	if err == nil || !strings.Contains(err.Error(), "provider profile missing_profile not found") {
		t.Fatalf("error = %v, want missing provider failure", err)
	}

	runs, err := repos.Runs.ListBySession(session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want 1", len(runs))
	}
	if runs[0].Status != "failed" || runs[0].FinishedAt == nil {
		t.Fatalf("admitted run was not finished after preparation failure: %#v", runs[0])
	}
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
	runtime := useStdioRuntimeHelper(t, capturePath)
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
	var params methods.RunExecuteParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Session.Conversation) != 200 {
		t.Fatalf("conversation len = %d, want 200", len(params.Session.Conversation))
	}
	assertConversationMessage(t, params.Session.Conversation[0], "user", "message 006")
	assertConversationMessage(t, params.Session.Conversation[199], "user", "message 205")
}

func TestNormalizedRuntimeModeDefaultsToPerRunProcess(t *testing.T) {
	if got := normalizedRuntimeMode(""); got != "per_run_process" {
		t.Fatalf("empty mode = %q, want per_run_process", got)
	}
	if got := normalizedRuntimeMode("auto"); got != "per_run_process" {
		t.Fatalf("auto mode = %q, want per_run_process", got)
	}
	if got := normalizedRuntimeMode("single_core"); got != "single_core" {
		t.Fatalf("single_core = %q", got)
	}
	if got := normalizedRuntimeMode("per_run_process"); got != "per_run_process" {
		t.Fatalf("per_run_process = %q", got)
	}
}

func TestRunServiceStartReservesSlotBeforeRuntimeAccept(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_reserve", "Reserve", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	runtime := useStdioRuntimeHelper(t, capturePath)
	service := NewRunService(repos, eventhub.New(), runtime)

	result, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "hello isolation"},
		Options:   map[string]any{"runtime_mode": "single_core"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeMode != "single_core" {
		t.Fatalf("runtime mode = %q, want single_core", result.RuntimeMode)
	}
	row, err := repos.Runs.Get(result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "running" {
		t.Fatalf("status = %q, want running", row.Status)
	}
	if row.RuntimeMode != "single_core" {
		t.Fatalf("stored runtime mode = %q, want single_core", row.RuntimeMode)
	}
}

func TestRunServiceStartDefaultRuntimeModeIsPerRunProcess(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_default_mode", "Default mode", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	runtime := useStdioRuntimeHelper(t, capturePath)
	service := NewRunService(repos, eventhub.New(), runtime)

	result, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "default mode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeMode != "per_run_process" {
		t.Fatalf("runtime mode = %q, want per_run_process", result.RuntimeMode)
	}
}

func TestRunServiceUnexpectedPerRunExitReleasesConcurrentSlot(t *testing.T) {
	repos, _ := newRunServiceTestFixture(t)
	session, err := repos.Sessions.Ensure("session_runtime_exit", "Runtime exit", "D:/workspace")
	if err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "reply-params.json")
	runtime := useStdioRuntimeHelper(t, capturePath)
	service := NewRunService(repos, eventhub.New(), runtime)
	result, err := service.Start(context.Background(), protows.RunStartPayload{
		SessionID: session.ID,
		Input:     map[string]any{"text": "runtime exits after accepting"},
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		row, getErr := repos.Runs.Get(result.RunID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if row.Status == "failed" {
			if row.FinishedAt == nil || !strings.Contains(row.Error, "runtime process exited") {
				t.Fatalf("unexpected recovered row: %#v", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run remained active after dedicated runtime exit: %#v", row)
		}
		time.Sleep(10 * time.Millisecond)
	}
	active, err := repos.Runs.CountActive()
	if err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active runs after runtime exit = %d, want 0", active)
	}
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
	runtime := useStdioRuntimeHelper(t, capturePath)
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
	t.Setenv("RED_PANDA_MAX_CONCURRENT_RUNS", "")
	runtime := useStdioRuntimeHelper(t, capturePath)
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
				ProtocolVersion: events.ProtocolVersionV2,
				Server:          methods.PeerInfo{Name: "test-runtime", Version: "test"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
		case methods.RunExecute:
			if err := os.WriteFile(os.Getenv("RED_PANDA_RUNTIME_CAPTURE"), request.Params, 0o600); err != nil {
				t.Fatal(err)
			}
			var params methods.RunExecuteParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			response, err := jsonrpc.NewResult(request.ID, methods.RunExecuteResult{
				Accepted:     true,
				RunID:        params.RunID,
				AssignmentID: "assignment_entry",
				WorkerID:     "worker_1",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
			return
		case methods.RunCancel:
			if err := os.WriteFile(os.Getenv("RED_PANDA_RUNTIME_CAPTURE"), request.Params, 0o600); err != nil {
				t.Fatal(err)
			}
			var params methods.RunCancelParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			response, err := jsonrpc.NewResult(request.ID, methods.RunCancelResult{
				Accepted:  true,
				RunID:     params.RunID,
				Cancelled: 1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
			return
		case methods.RunPause:
			if err := os.WriteFile(os.Getenv("RED_PANDA_RUNTIME_CAPTURE")+".pause", request.Params, 0o600); err != nil {
				t.Fatal(err)
			}
			var params methods.RunPauseParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			paused := 1
			if os.Getenv("RED_PANDA_RUNTIME_PAUSED_COUNT") == "0" {
				paused = 0
			}
			response, err := jsonrpc.NewResult(request.ID, methods.RunPauseResult{
				Accepted: true,
				RunID:    params.RunID,
				Paused:   paused,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
		case methods.RunResume:
			if err := os.WriteFile(os.Getenv("RED_PANDA_RUNTIME_CAPTURE")+".resume", request.Params, 0o600); err != nil {
				t.Fatal(err)
			}
			if os.Getenv("RED_PANDA_RUNTIME_RESUME_ERROR") == "1" {
				if err := encoder.Encode(jsonrpc.NewError(request.ID, -32021, "resume failed")); err != nil {
					t.Fatal(err)
				}
				continue
			}
			var params methods.RunResumeParams
			if err := json.Unmarshal(request.Params, &params); err != nil {
				t.Fatal(err)
			}
			response, err := jsonrpc.NewResult(request.ID, methods.RunResumeResult{
				Accepted: true,
				RunID:    params.RunID,
				Resumed:  1,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := encoder.Encode(response); err != nil {
				t.Fatal(err)
			}
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
	service.HandleRuntimeEvent(events.EnvelopeV2{
		EventID:      "evt_perm",
		RunID:        "run_1",
		SessionID:    "session_1",
		AssignmentID: "assignment_1",
		RunSeq:       1,
		WorkerSeq:    1,
		Worker:       events.EventWorkerRef{ID: "worker-01"},
		Type:         events.EventPermissionRequest,
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

func TestRunServiceAggregatesConversationMessagesAndHidesWorkerPrivateDeltas(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	if _, err := repos.Messages.Add("session_1", "user", "prompt", "run_1"); err != nil {
		t.Fatal(err)
	}

	service.HandleRuntimeEvent(messageDeltaEvent("evt_1", "run_1", "session_1", 1, false, "hello "))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_2", "run_1", "session_1", 2, false, "world"))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_3", "run_2", "session_1", 1, false, "new run"))
	service.HandleRuntimeEvent(messageDeltaEvent("evt_4", "run_2", "session_1", 2, true, "private plan"))

	rows, err := repos.Messages.List("session_1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	assertServiceMessage(t, rows[0], "user", "run_1", "prompt")
	assertServiceMessage(t, rows[1], "assistant", "run_1", "hello world")
	assertServiceMessage(t, rows[2], "assistant", "run_2", "new run")

	history, err := NewSessionService(repos, nil, nil, "").History("session_1", 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	if history.Items[1].WorkerID != "worker-01" || history.Items[1].AssignmentID != "assignment_run_1" || history.Items[1].ProfileKey != "general" {
		t.Fatalf("history lost Worker attribution: %#v", history.Items[1])
	}
}

func TestRunServiceApplyProviderProfile(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	profile, err := repos.Providers.Create(model.ProviderProfile{
		Name:     "openai",
		Provider: "openai_compatible",
		BaseURL:  "https://provider.invalid/v1",
		Model:    "profile-model",
		Models: []model.ProviderModel{
			{Model: "profile-model", ReasoningEffort: "medium"},
			{Model: "explicit-model", ReasoningEffort: "high"},
		},
		APIKeySecret: "sk-profile",
		IsDefault:    true,
		Stream:       false,
	})
	if err != nil {
		t.Fatal(err)
	}

	params := methods.RunExecuteParams{
		Options: methods.RunExecuteOptions{
			ProviderProfileID: profile.ID,
		},
	}
	if err := service.applyProviderProfile(&params); err != nil {
		t.Fatal(err)
	}
	if params.Options.ProviderName != "openai_compatible" ||
		params.Options.ProviderBaseURL != "https://provider.invalid/v1" ||
		params.Options.ProviderAPIKey != "sk-profile" ||
		params.Options.ProviderStream == nil || *params.Options.ProviderStream ||
		params.Options.Model != "profile-model" || params.Options.ReasoningEffort != "medium" {
		t.Fatalf("provider profile options mismatch: %#v", params.Options)
	}

	params.Options.Model = "explicit-model"
	params.Options.ReasoningEffort = "low"
	if err := service.applyProviderProfile(&params); err != nil {
		t.Fatal(err)
	}
	if params.Options.Model != "explicit-model" || params.Options.ReasoningEffort != "low" {
		t.Fatalf("explicit run model/effort should win, got %#v", params.Options)
	}
	params.Options.Model = "unknown"
	if err := service.applyProviderProfile(&params); err == nil {
		t.Fatal("unconfigured model should be rejected")
	}
}

func TestRunServiceApplyWorkerProfilesUsesWorkerProfileSourceOfTruth(t *testing.T) {
	repos, service := newRunServiceTestFixture(t)
	workerSvc := NewWorkerProfileService(repos)
	created, err := workerSvc.Create(WorkerProfileCreate{
		Key: "custom-worker", Name: "Custom Worker", Phase: "build",
		SystemPrompt: "WORKER PROFILE AUTHORITATIVE PROMPT", DefaultMaxTurns: 9,
		Provider: "openai_compatible", Model: "worker-model",
		ToolAllowlist: []string{"workspace.read_file"}, ToolDenylist: []string{"shell.exec"},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := repos.WorkerProfiles.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repos.WorkerProfiles.Update(row); err != nil {
		t.Fatal(err)
	}
	params := methods.RunExecuteParams{Session: methods.ReplySession{ID: "sess_workers"}}
	if err := service.applyWorkerProfiles(&params); err != nil {
		t.Fatal(err)
	}
	// Custom profile is enabled by default and should be attached.
	if len(params.Options.WorkerProfiles) != 1 {
		t.Fatalf("expected 1 default-enabled worker profile, got %d", len(params.Options.WorkerProfiles))
	}
	var found *methods.WorkerProfileRef
	for i := range params.Options.WorkerProfiles {
		if params.Options.WorkerProfiles[i].Key == "custom-worker" {
			found = &params.Options.WorkerProfiles[i]
			break
		}
	}
	if found == nil {
		t.Fatal("custom-worker missing from run options")
	}
	if found.SystemPrompt != "WORKER PROFILE AUTHORITATIVE PROMPT" || found.DefaultMaxTurns != 9 {
		t.Fatalf("worker profile not attached: %#v", found)
	}
	if found.ProviderName != "openai_compatible" || found.Model != "worker-model" || found.ToolPolicy != "risk_based" {
		t.Fatalf("worker execution policy mismatch: %#v", found)
	}
	denyHasShell := false
	for _, tool := range found.ToolDenylist {
		denyHasShell = denyHasShell || tool == "shell.exec"
	}
	if len(found.ToolAllowlist) != 1 || found.ToolAllowlist[0] != "workspace.read_file" || !denyHasShell {
		t.Fatalf("worker tool policy lists mismatch: %#v", found)
	}
	if strings.Contains(found.SystemPrompt, "LEGACY AGENT") {
		t.Fatalf("legacy agent definition leaked into worker profile: %#v", found)
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

	params := methods.RunExecuteParams{
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
	service.HandleRuntimeEvent(events.EnvelopeV2{
		EventID:      "evt_memory",
		RunID:        "run_memory",
		SessionID:    "session_memory",
		AssignmentID: "assignment_memory",
		RunSeq:       1,
		WorkerSeq:    1,
		Worker:       events.EventWorkerRef{ID: "worker-01"},
		Type:         events.EventMemoryInjected,
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
	if err := repos.RunEvents.Save(events.EnvelopeV2{
		EventID:      "evt_1",
		RunID:        "run_events",
		SessionID:    "session_1",
		AssignmentID: "assignment_1",
		RunSeq:       1,
		WorkerSeq:    1,
		Worker:       events.EventWorkerRef{ID: "worker-01", ProfileKey: "general"},
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
	if items[0].ID != "evt_1" || items[0].RunSeq != 1 || items[0].WorkerID != "worker-01" || items[0].AssignmentID != "assignment_1" || items[0].StreamKind != "message" {
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

// useStdioRuntimeHelper forces stdio transport for TestRunServiceRuntimeHelperProcess.
// Production defaults to IPC; the helper only speaks NDJSON over stdio.
func useStdioRuntimeHelper(t *testing.T, capturePath string) *runtimeclient.Client {
	t.Helper()
	t.Setenv("RED_PANDA_RUNTIME_IPC", "stdio")
	t.Setenv("RED_PANDA_RUNTIME_HELPER", "1")
	t.Setenv("RED_PANDA_RUNTIME_CAPTURE", capturePath)
	return runtimeclient.New(os.Args[0], []string{"-test.run=TestRunServiceRuntimeHelperProcess"}, "test", nil, nil)
}

func messageDeltaEvent(eventID string, runID string, sessionID string, runSeq uint64, workerPrivate bool, delta string) events.EnvelopeV2 {
	payload := map[string]any{"delta": delta}
	profileKey := "general"
	if workerPrivate {
		payload["visibility"] = "worker_private"
		profileKey = "planner"
	}
	return events.EnvelopeV2{
		EventID:      eventID,
		RunID:        runID,
		SessionID:    sessionID,
		AssignmentID: "assignment_" + runID,
		RunSeq:       runSeq,
		WorkerSeq:    runSeq,
		Worker:       events.EventWorkerRef{ID: "worker-01", ProfileKey: profileKey},
		Type:         events.EventMessageDelta,
		Payload:      payload,
		CreatedAt:    time.Now().UTC(),
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
