package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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
	summary := summarizeMessages([]model.Message{
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

	preview, err := service.CompactPreview(source.ID, CompactPreviewRequest{
		KeepTailMessages: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Preview.SourceStartSeq != 1 || preview.Preview.SourceEndSeq != 3 ||
		preview.Preview.KeepTailMessages != 1 || preview.Preview.Summary.Summary == "" {
		t.Fatalf("preview mismatch: %#v", preview)
	}
	sessionsBefore, err := repos.Sessions.List(10)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.Compact(source.ID, CompactSessionRequest{
		Name:             "compact",
		KeepTailMessages: 1,
		Summary:          preview.Preview.Summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ParentID != source.ID || result.Session.Kind != "compact" ||
		result.CopiedTailMessages != 1 || result.Compaction.Status != "applied" {
		t.Fatalf("compact result mismatch: %#v", result)
	}
	if result.Lineage.Operation != "compact" || result.Lineage.SourceStartSeq != 1 || result.Lineage.SourceEndSeq != 3 {
		t.Fatalf("compact lineage mismatch: %#v", result.Lineage)
	}

	compactMessages, err := repos.Messages.List(result.Session.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(compactMessages) != 2 {
		t.Fatalf("len(compactMessages) = %d, want 2", len(compactMessages))
	}
	assertSessionTestMessageText(t, compactMessages[0], preview.Preview.Summary.Summary)
	assertSessionTestMessageText(t, compactMessages[1], "second answer")
	if compactMessages[0].MetadataJSON == "" || compactMessages[1].SourceMessageID == "" {
		t.Fatalf("compact message metadata mismatch: %#v", compactMessages)
	}

	sessionsAfter, err := repos.Sessions.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionsAfter) != len(sessionsBefore)+1 {
		t.Fatalf("preview should not create sessions and apply should create one, before=%d after=%d", len(sessionsBefore), len(sessionsAfter))
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
	return repos, NewSessionService(repos)
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
