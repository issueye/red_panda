package service

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

func newWorkerProfileTestService(t *testing.T) (WorkerProfileService, repository.Set) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "worker-profiles.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.WorkerProfile{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewSet(db)
	return NewWorkerProfileService(repos), repos
}

func TestWorkerProfileEnsureBuiltinsUsesWorkerDelegateDenylist(t *testing.T) {
	svc, repos := newWorkerProfileTestService(t)
	if err := svc.EnsureBuiltins(); err != nil {
		t.Fatal(err)
	}
	rows, err := repos.WorkerProfiles.List(200)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("builtin count = %d, want 5", len(rows))
	}
	for _, row := range rows {
		if !row.Builtin || row.Kind != "builtin" {
			t.Fatalf("unexpected builtin row: %#v", row)
		}
		if !containsWorkerProfileTool(row.ToolDenylist, "worker.delegate") {
			t.Fatalf("%s does not deny worker.delegate: %#v", row.Key, row.ToolDenylist)
		}
		if containsWorkerProfileTool(row.ToolDenylist, "subagent.run") {
			t.Fatalf("%s still references subagent.run: %#v", row.Key, row.ToolDenylist)
		}
	}

	analyst, err := repos.WorkerProfiles.GetByKey("goal-analyst")
	if err != nil {
		t.Fatal(err)
	}
	analyst.Name = "Operator Analyst"
	analyst.NameZH = "自定义分析师"
	analyst.Description = "operator description"
	analyst.SystemPrompt = "operator prompt"
	analyst.Provider = "openai"
	analyst.Model = "gpt-5"
	analyst.DefaultMaxTurns = 19
	analyst.ToolAllowlist = []string{"workspace.read_file"}
	analyst.ToolDenylist = []string{"shell.exec"}
	analyst.Enabled = false
	if _, err := repos.WorkerProfiles.Update(analyst); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureBuiltins(); err != nil {
		t.Fatal(err)
	}
	reseeded, err := repos.WorkerProfiles.GetByKey("goal-analyst")
	if err != nil {
		t.Fatal(err)
	}
	if reseeded.Enabled {
		t.Fatal("reseed did not preserve builtin enabled toggle")
	}
	if !containsWorkerProfileTool(reseeded.ToolDenylist, "worker.delegate") {
		t.Fatalf("reseed did not restore worker.delegate deny: %#v", reseeded.ToolDenylist)
	}
	if reseeded.Name != analyst.Name || reseeded.NameZH != analyst.NameZH ||
		reseeded.Description != analyst.Description || reseeded.SystemPrompt != analyst.SystemPrompt ||
		reseeded.Provider != analyst.Provider || reseeded.Model != analyst.Model ||
		reseeded.DefaultMaxTurns != analyst.DefaultMaxTurns ||
		!reflect.DeepEqual(reseeded.ToolAllowlist, analyst.ToolAllowlist) ||
		!containsWorkerProfileTool(reseeded.ToolDenylist, "shell.exec") {
		t.Fatalf("reseed overwrote operator settings: %#v", reseeded)
	}
	if _, err := svc.List(); err != nil {
		t.Fatal(err)
	}
	afterList, err := svc.Get(reseeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterList.SystemPrompt != analyst.SystemPrompt || afterList.Provider != analyst.Provider ||
		afterList.Model != analyst.Model || afterList.DefaultMaxTurns != analyst.DefaultMaxTurns {
		t.Fatalf("List/Get reseed discarded operator settings: %#v", afterList)
	}
	legacyTool := "subagent." + "run"
	updated, err := svc.Update(reseeded.ID, WorkerProfileUpdate{ToolDenylist: &[]string{legacyTool}})
	if err != nil {
		t.Fatal(err)
	}
	if containsWorkerProfileTool(updated.ToolDenylist, legacyTool) || !containsWorkerProfileTool(updated.ToolDenylist, "worker.delegate") {
		t.Fatalf("builtin update did not normalize denylist: %#v", updated.ToolDenylist)
	}
}

func TestWorkerProfileEnsureBuiltinsIsConcurrentSafe(t *testing.T) {
	svc, repos := newWorkerProfileTestService(t)
	const callers = 16
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.EnsureBuiltins()
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent EnsureBuiltins failed: %v", err)
		}
	}
	rows, err := repos.WorkerProfiles.List(200)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(builtinWorkerProfileSeeds()) {
		t.Fatalf("profile count = %d, want %d", len(rows), len(builtinWorkerProfileSeeds()))
	}
}

func TestWorkerProfileDeleteAllowsKeyReuse(t *testing.T) {
	svc, _ := newWorkerProfileTestService(t)
	first, err := svc.Create(WorkerProfileCreate{Key: "reusable-worker", Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(WorkerProfileCreate{Key: first.Key, Name: "Second"})
	if err != nil {
		t.Fatalf("recreate after soft delete failed: %v", err)
	}
	if second.ID == first.ID || second.Name != "Second" {
		t.Fatalf("unexpected recreated profile: %#v", second)
	}
}

func TestWorkerProfileCRUDSupportsProviderAndModel(t *testing.T) {
	svc, _ := newWorkerProfileTestService(t)
	enabled := true
	created, err := svc.Create(WorkerProfileCreate{
		Key: "review-worker", Name: "Reviewer", Phase: "verify",
		Provider: "openai", Model: "gpt-5.1", SystemPrompt: "Review changes.",
		ToolAllowlist:   []string{"workspace.read_file", "workspace.read_file"},
		DefaultMaxTurns: 7, Enabled: &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "openai" || created.Model != "gpt-5.1" || created.Kind != "custom" {
		t.Fatalf("unexpected create: %#v", created)
	}
	provider, modelName, disabled := "anthropic", "claude-sonnet-4", false
	updated, err := svc.Update(created.ID, WorkerProfileUpdate{Provider: &provider, Model: &modelName, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Provider != provider || updated.Model != modelName || updated.Enabled {
		t.Fatalf("unexpected update: %#v", updated)
	}
	got, err := svc.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != provider || got.Model != modelName {
		t.Fatalf("unexpected get: %#v", got)
	}
	enabledItems, err := svc.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range enabledItems {
		if item.ID == created.ID {
			t.Fatal("disabled worker profile returned by ListEnabled")
		}
	}
	if err := svc.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(created.ID); err == nil {
		t.Fatal("deleted worker profile is still readable")
	}
}

func TestWorkerProfileReseedDoesNotOverwriteCustomProfile(t *testing.T) {
	svc, repos := newWorkerProfileTestService(t)
	created, err := svc.Create(WorkerProfileCreate{
		Key: "custom-builder", Name: "Custom Builder", Phase: "execute",
		Description: "operator description", SystemPrompt: "operator prompt",
		Provider: "local", Model: "custom-model", ToolDenylist: []string{"dangerous.tool"},
		DefaultMaxTurns: 31, MetadataJSON: `{"owner":"user"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repos.WorkerProfiles.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureBuiltins(); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureBuiltins(); err != nil {
		t.Fatal(err)
	}
	after, err := repos.WorkerProfiles.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	before.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("custom profile changed during reseed\nbefore=%#v\nafter=%#v", before, after)
	}
}

func containsWorkerProfileTool(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
