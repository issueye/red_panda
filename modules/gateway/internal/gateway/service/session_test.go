package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

func TestSummarizeMessagesExcludesSubagentContent(t *testing.T) {
	encode := func(text string) string {
		raw, err := json.Marshal([]methods.ContentBlock{{Type: "text", Text: text}})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	summary := summarizeMessagesLocal([]model.Message{
		{Role: "user", ContentJSON: encode("root question")},
		{Role: "subagent", ContentJSON: encode("PRIVATE_SUBAGENT_SENTINEL")},
		{Role: "assistant", ContentJSON: encode("root answer")},
	}, 1, 3)
	if !strings.Contains(summary.Summary, "root question") || !strings.Contains(summary.Summary, "root answer") {
		t.Fatalf("summary missing root conversation: %q", summary.Summary)
	}
	if strings.Contains(summary.Summary, "PRIVATE_SUBAGENT_SENTINEL") {
		t.Fatalf("summary leaked subagent content: %q", summary.Summary)
	}
}

func TestPauseSessionForCompactPausesAndResumesDelegatedWorkers(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	session, err := repos.Sessions.Create("compact active run", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID:        "run_compact_v2",
		SessionID: session.ID,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(t.TempDir(), "run-compact-params.json")
	runtime := useStdioRuntimeHelper(t, capturePath)
	service := NewSessionService(repos, runtime)

	paused, err := service.pauseSessionForCompact(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.paused != 1 {
		t.Fatalf("paused = %d, want 1", paused.paused)
	}
	if len(paused.runIDs) != 1 || paused.runIDs[0] != "run_compact_v2" {
		t.Fatalf("paused run ids = %#v", paused.runIDs)
	}
	raw, err := os.ReadFile(capturePath + ".pause")
	if err != nil {
		t.Fatal(err)
	}
	var params methods.RunPauseParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	if params.RunID != "run_compact_v2" || params.Reason != "session compact" || !params.DelegatedOnly {
		t.Fatalf("run.pause params = %#v", params)
	}
	if err := service.resumeSessionAfterCompact(session.ID); err != nil {
		t.Fatal(err)
	}
	resumeRaw, err := os.ReadFile(capturePath + ".resume")
	if err != nil {
		t.Fatal(err)
	}
	var resumeParams methods.RunResumeParams
	if err := json.Unmarshal(resumeRaw, &resumeParams); err != nil {
		t.Fatal(err)
	}
	if resumeParams.RunID != "run_compact_v2" || !resumeParams.DelegatedOnly {
		t.Fatalf("run.resume_execution params = %#v", resumeParams)
	}
	run, err := repos.Runs.Get("run_compact_v2")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

func TestSessionServiceForkCopiesMessagesAndLineage(t *testing.T) {
	repos, service := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("source", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(source.ID, "user", "one", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(source.ID, "assistant", "two", "run_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repos.Messages.Add(source.ID, "user", "three", "run_2"); err != nil {
		t.Fatal(err)
	}

	result, err := service.Fork(source.ID, ForkSessionRequest{
		Name:      "fork",
		ForkPoint: ForkPoint{MessageSeq: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ParentID != source.ID || result.Session.Kind != "fork" || result.CopiedMessages != 2 {
		t.Fatalf("fork result mismatch: %#v", result)
	}
	if result.Lineage.Operation != "fork" || result.Lineage.SourceSessionID != source.ID ||
		result.Lineage.TargetSessionID != result.Session.ID || result.Lineage.ForkPointSeq != 2 {
		t.Fatalf("lineage mismatch: %#v", result.Lineage)
	}

	forkMessages, err := repos.Messages.List(result.Session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(forkMessages) != 2 {
		t.Fatalf("len(forkMessages) = %d, want 2", len(forkMessages))
	}
	assertSessionTestMessageText(t, forkMessages[0], "one")
	assertSessionTestMessageText(t, forkMessages[1], "two")
	if forkMessages[0].SourceMessageID == "" || forkMessages[1].SourceMessageID == "" {
		t.Fatalf("copied messages missing source ids: %#v", forkMessages)
	}

	sourceMessages, err := repos.Messages.List(source.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceMessages) != 3 {
		t.Fatalf("source was mutated, len = %d", len(sourceMessages))
	}
}

func TestSessionServiceCompactPreviewAndApply(t *testing.T) {
	repos, service := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("source", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		role string
		text string
		run  string
	}{
		{"user", "first prompt", "run_1"},
		{"assistant", "first answer", "run_1"},
		{"user", "second prompt", "run_2"},
		{"assistant", "second answer", "run_2"},
	} {
		if _, err := repos.Messages.Add(source.ID, item.role, item.text, item.run); err != nil {
			t.Fatal(err)
		}
	}

	// Explicit keep_tail_messages forces message-count mode (legacy contract).
	preview, err := service.CompactPreview(source.ID, CompactPreviewRequest{
		KeepTailMessages: 1,
		Mode:             "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Preview.SourceStartSeq != 1 || preview.Preview.SourceEndSeq != 3 ||
		preview.Preview.KeepTailMessages != 1 || preview.Preview.Summary.Summary == "" {
		t.Fatalf("preview mismatch: %#v", preview)
	}
	if preview.Preview.SummaryMethod != "local" {
		t.Fatalf("summary_method = %q, want local", preview.Preview.SummaryMethod)
	}
	sessionsBefore, err := repos.Sessions.List(10)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Compact(source.ID, CompactSessionRequest{
		KeepTailMessages: 1,
		Mode:             "local",
		Summary:          preview.Preview.Summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ID != source.ID || result.Session.Kind != "normal" ||
		result.KeepTailMessages != 1 || result.Compaction.Status != "applied" ||
		result.Compaction.TargetSessionID != source.ID {
		t.Fatalf("compact result mismatch: %#v", result)
	}

	originalMessages, err := repos.Messages.List(source.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(originalMessages) != 4 {
		t.Fatalf("compaction mutated history, len(messages) = %d, want 4", len(originalMessages))
	}
	assertSessionTestMessageText(t, originalMessages[0], "first prompt")
	assertSessionTestMessageText(t, originalMessages[3], "second answer")
	state, err := service.CompactionState(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Compaction == nil || state.Compaction.ID != result.Compaction.ID || state.Summary.Summary == "" {
		t.Fatalf("compaction state mismatch: %#v", state)
	}

	sessionsAfter, err := repos.Sessions.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionsAfter) != len(sessionsBefore) {
		t.Fatalf("compaction must not create a session, before=%d after=%d", len(sessionsBefore), len(sessionsAfter))
	}
}

func TestRunConversationUsesSummarySnapshotAndPreservesTail(t *testing.T) {
	repos, sessionService := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("source", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		role string
		text string
	}{
		{"user", "old question"},
		{"assistant", "old answer"},
		{"user", "recent question"},
		{"assistant", "recent answer"},
	} {
		if _, err := repos.Messages.Add(source.ID, item.role, item.text, "run"); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := sessionService.CompactPreview(source.ID, CompactPreviewRequest{KeepTailMessages: 2, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessionService.Compact(source.ID, CompactSessionRequest{
		KeepTailMessages: 2,
		Mode:             "local",
		Summary:          preview.Preview.Summary,
	}); err != nil {
		t.Fatal(err)
	}

	conversation, err := (RunService{repos: repos}).buildRunConversation(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 3 || conversation[0].Role != "system" ||
		conversation[1].Role != "user" || conversation[2].Role != "assistant" {
		t.Fatalf("conversation shape = %#v", conversation)
	}
	if !strings.Contains(conversation[0].Content[0].Text, "old question") ||
		conversation[1].Content[0].Text != "recent question" ||
		conversation[2].Content[0].Text != "recent answer" {
		t.Fatalf("conversation content = %#v", conversation)
	}
}

func TestSessionServiceCompactSupersedesPreviousSnapshot(t *testing.T) {
	repos, sessionService := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("source", "D:\\workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two", "three", "four"} {
		role := "user"
		if text == "two" || text == "four" {
			role = "assistant"
		}
		if _, err := repos.Messages.Add(source.ID, role, text, "run"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := sessionService.Compact(source.ID, CompactSessionRequest{
		KeepTailMessages: 2,
		Mode:             "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionService.Compact(source.ID, CompactSessionRequest{
		KeepTailMessages: 1,
		Mode:             "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Compaction.ID == second.Compaction.ID {
		t.Fatal("expected a new snapshot")
	}
	rows, err := repos.Compactions.ListForSession(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Status != "superseded" || rows[1].Status != "applied" {
		t.Fatalf("snapshot statuses = %#v", rows)
	}
}

func newSessionServiceTestFixture(t *testing.T) (repository.Set, SessionService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{})
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
	return repos, NewSessionService(repos, nil)
}

func TestSessionServiceCompactPausesActiveRuns(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("compact-pause", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"user goal", "assistant progress", "user more", "assistant more"} {
		role := "user"
		if strings.HasPrefix(text, "assistant") {
			role = "assistant"
		}
		if _, err := repos.Messages.Add(source.ID, role, text, "run_active"); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID:        "run_active",
		SessionID: source.ID,
		Status:    "running",
		Input:     "user goal",
	}); err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "compact-active-run.json")
	service := NewSessionService(repos, useStdioRuntimeHelper(t, capturePath))
	result, err := service.Compact(source.ID, CompactSessionRequest{
		KeepTailTurns: 1,
		Mode:          "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PausedRuns != 1 {
		t.Fatalf("PausedRuns = %d, want 1", result.PausedRuns)
	}
	active, err := repos.Runs.CountActiveBySession(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active runs after compact = %d, want 1", active)
	}
	run, err := repos.Runs.Get("run_active")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
	if _, err := os.Stat(capturePath + ".pause"); err != nil {
		t.Fatalf("pause RPC was not captured: %v", err)
	}
	if _, err := os.Stat(capturePath + ".resume"); err != nil {
		t.Fatalf("resume RPC was not captured: %v", err)
	}
}

func TestSessionServiceCompactResumesWorkersWhenSummaryFails(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("compact-resume-on-error", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID: "run_resume_on_error", SessionID: source.ID, Status: "running", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	capturePath := filepath.Join(t.TempDir(), "compact-error.json")
	service := NewSessionService(repos, useStdioRuntimeHelper(t, capturePath))

	if _, err := service.Compact(source.ID, CompactSessionRequest{Mode: "local"}); err == nil {
		t.Fatal("compact succeeded without any messages")
	}
	if _, err := os.Stat(capturePath + ".pause"); err != nil {
		t.Fatalf("pause RPC was not captured: %v", err)
	}
	if _, err := os.Stat(capturePath + ".resume"); err != nil {
		t.Fatalf("resume RPC was not captured after compact failure: %v", err)
	}
	run, err := repos.Runs.Get("run_resume_on_error")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("run status = %q, want running", run.Status)
	}
}

func TestSessionServiceCompactResumesRunsWhenPauseReportsZero(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("compact-resume-stale-pause", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"user goal", "assistant progress", "user more", "assistant more"} {
		role := "user"
		if strings.HasPrefix(text, "assistant") {
			role = "assistant"
		}
		if _, err := repos.Messages.Add(source.ID, role, text, "run_stale_pause"); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID: "run_stale_pause", SessionID: source.ID, Status: "running", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "compact-stale-pause.json")
	t.Setenv("RED_PANDA_RUNTIME_PAUSED_COUNT", "0")
	service := NewSessionService(repos, useStdioRuntimeHelper(t, capturePath))
	result, err := service.Compact(source.ID, CompactSessionRequest{KeepTailTurns: 1, Mode: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if result.PausedRuns != 0 {
		t.Fatalf("PausedRuns = %d, want 0", result.PausedRuns)
	}
	if _, err := os.Stat(capturePath + ".resume"); err != nil {
		t.Fatalf("resume RPC was not sent for an active run after a zero-count pause: %v", err)
	}
}

func TestSessionServiceCompactReportsResumeFailure(t *testing.T) {
	repos, _ := newSessionServiceTestFixture(t)
	source, err := repos.Sessions.Create("compact-resume-failure", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"user goal", "assistant progress", "user more", "assistant more"} {
		role := "user"
		if strings.HasPrefix(text, "assistant") {
			role = "assistant"
		}
		if _, err := repos.Messages.Add(source.ID, role, text, "run_resume_failure"); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Runs.Start(model.RunRecord{
		ID: "run_resume_failure", SessionID: source.ID, Status: "running", StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	capturePath := filepath.Join(t.TempDir(), "compact-resume-failure.json")
	t.Setenv("RED_PANDA_RUNTIME_RESUME_ERROR", "1")
	service := NewSessionService(repos, useStdioRuntimeHelper(t, capturePath))
	_, err = service.Compact(source.ID, CompactSessionRequest{KeepTailTurns: 1, Mode: "local"})
	if err == nil || !strings.Contains(err.Error(), "resume session after compact") {
		t.Fatalf("compact error = %v, want explicit resume failure", err)
	}
}

func assertSessionTestMessageText(t *testing.T, row model.Message, want string) {
	t.Helper()
	var content []methods.ContentBlock
	if err := json.Unmarshal([]byte(row.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Text != want {
		t.Fatalf("message text = %#v, want %q", content, want)
	}
}
