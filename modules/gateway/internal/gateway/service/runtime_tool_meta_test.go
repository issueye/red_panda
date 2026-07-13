package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/repository"
)

func newRuntimeToolMetaRepos(t *testing.T) repository.Set {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "meta.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return repository.NewSet(db)
}

func TestValidateRuntimeToolMetaRequiresRunAndToolCall(t *testing.T) {
	repos := newRuntimeToolMetaRepos(t)
	if err := validateRuntimeToolMeta(repos, "", "sess", "tc1", false); err == nil || !strings.Contains(err.Error(), "run_id") {
		t.Fatalf("want run_id error, got %v", err)
	}
	if err := validateRuntimeToolMeta(repos, "run1", "sess", "", false); err == nil || !strings.Contains(err.Error(), "tool_call_id") {
		t.Fatalf("want tool_call_id error, got %v", err)
	}
	if err := validateRuntimeToolMeta(repos, "run1", "", "tc1", false); err != nil {
		t.Fatalf("memory-style (no session) should pass: %v", err)
	}
}

func TestValidateRuntimeToolMetaRequiresExistingSession(t *testing.T) {
	repos := newRuntimeToolMetaRepos(t)
	if err := validateRuntimeToolMeta(repos, "run1", "", "tc1", true); err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Fatalf("want session_id error, got %v", err)
	}
	if err := validateRuntimeToolMeta(repos, "run1", "missing", "tc1", true); err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("want session not found, got %v", err)
	}
	session, err := repos.Sessions.Create("meta-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRuntimeToolMeta(repos, "run1", session.ID, "tc1", true); err != nil {
		t.Fatalf("valid session should pass: %v", err)
	}
}
