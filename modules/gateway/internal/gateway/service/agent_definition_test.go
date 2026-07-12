package service

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/repository"
)

func newAgentTestService(t *testing.T) AgentDefinitionService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "agents.db")), &gorm.Config{})
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
	return NewAgentDefinitionService(repository.NewSet(db))
}

func TestAgentDefinitionEnsureBuiltinsAndList(t *testing.T) {
	svc := newAgentTestService(t)
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 5 {
		t.Fatalf("expected >=5 builtins, got %d", len(items))
	}
	keys := map[string]bool{}
	for _, item := range items {
		keys[item.Key] = true
		if !item.Builtin {
			t.Fatalf("seed item should be builtin: %#v", item)
		}
		if !item.Enabled {
			t.Fatalf("builtin should start enabled: %s", item.Key)
		}
	}
	for _, key := range []string{"goal-analyst", "goal-planner", "goal-implementer", "goal-verifier", "goal-evaluator"} {
		if !keys[key] {
			t.Fatalf("missing builtin %s", key)
		}
	}
}

func TestAgentDefinitionDisableBuiltinAndRejectDelete(t *testing.T) {
	svc := newAgentTestService(t)
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	var analyst AgentDefinitionDTO
	for _, item := range items {
		if item.Key == "goal-analyst" {
			analyst = item
			break
		}
	}
	if analyst.ID == "" {
		t.Fatal("analyst not found")
	}
	enabled := false
	updated, err := svc.Update(analyst.ID, AgentDefinitionUpdate{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("expected disabled")
	}
	if err := svc.Delete(analyst.ID); err == nil {
		t.Fatal("expected delete builtin to fail")
	}
}

func TestAgentDefinitionCreateCustomAndDelete(t *testing.T) {
	svc := newAgentTestService(t)
	enabled := true
	created, err := svc.Create(AgentDefinitionCreate{
		Key:             "my-helper",
		Name:            "My Helper",
		NameZH:          "我的助手",
		Phase:           "general",
		Description:     "custom",
		SystemPrompt:    "help",
		DefaultMaxTurns: 6,
		Enabled:         &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Kind != "custom" || created.Builtin || created.Key != "my-helper" {
		t.Fatalf("unexpected create: %#v", created)
	}
	// Reserved prefix
	if _, err := svc.Create(AgentDefinitionCreate{Key: "goal-foo", Name: "x"}); err == nil {
		t.Fatal("expected goal- prefix rejected")
	}
	if err := svc.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAgentDefinitionListEnabledFilters(t *testing.T) {
	svc := newAgentTestService(t)
	items, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	if _, err := svc.Update(items[0].ID, AgentDefinitionUpdate{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	live, err := svc.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range live {
		if item.ID == items[0].ID {
			t.Fatalf("disabled agent still in ListEnabled")
		}
		if !item.Enabled {
			t.Fatalf("ListEnabled returned disabled %#v", item)
		}
	}
}
