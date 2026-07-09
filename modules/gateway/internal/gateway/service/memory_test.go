package service

import (
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

func TestMemoryServiceCRUDValidationAndPreview(t *testing.T) {
	repos, service := newMemoryServiceTestFixture(t)

	if _, err := service.Create(MemoryCreateRequest{
		Scope:         "project",
		Content:       "Project memory",
		WorkspaceRoot: "D:/workspace",
		Source:        "user",
	}); err != nil {
		t.Fatal(err)
	}
	sessionMemory, err := service.Create(MemoryCreateRequest{
		Scope:      "session",
		Kind:       "decision",
		Title:      "Session decision",
		Content:    "Session memory",
		Confidence: "high",
		SessionID:  "session_1",
		Source:     "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := service.Create(MemoryCreateRequest{
		Scope:     "session",
		Content:   "Disabled memory",
		SessionID: "session_1",
		Status:    "disabled",
		Source:    "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := service.Create(MemoryCreateRequest{
		Scope:         "project",
		Content:       "Other workspace memory",
		WorkspaceRoot: "D:/other",
		Source:        "user",
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.Update(sessionMemory.ID, MemoryUpdateRequest{
		Content:    ptrString("Updated session memory"),
		Confidence: ptrString("medium"),
		Metadata:   map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "Updated session memory" || updated.Confidence != "medium" || updated.Metadata["source"] != "test" {
		t.Fatalf("update mismatch: %#v", updated)
	}

	deleted, err := service.Delete(otherProject.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Status != "deleted" {
		t.Fatalf("delete status = %s, want deleted", deleted.Status)
	}

	active, err := service.List(MemoryListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range active {
		if item.ID == disabled.ID || item.ID == deleted.ID || item.Status != "active" {
			t.Fatalf("default active list mismatch: %#v", active)
		}
	}

	preview, err := service.PreviewRun(MemoryPreviewRunRequest{
		SessionID:     "session_1",
		WorkspaceRoot: "D:/workspace",
		Input:         "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 2 {
		t.Fatalf("preview items len = %d, want 2: %#v", len(preview.Items), preview.Items)
	}
	if !strings.Contains(preview.Context, "Project memory") || !strings.Contains(preview.Context, "Updated session memory") {
		t.Fatalf("preview context missing active memory: %q", preview.Context)
	}
	if strings.Contains(preview.Context, "Disabled memory") || strings.Contains(preview.Context, "Other workspace memory") {
		t.Fatalf("preview context included inactive/mismatched memory: %q", preview.Context)
	}

	if _, err := repos.Memory.Get(sessionMemory.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryServiceRejectsInvalidTraceAndContent(t *testing.T) {
	_, service := newMemoryServiceTestFixture(t)
	cases := []MemoryCreateRequest{
		{Scope: "project", Content: "missing workspace", Source: "user"},
		{Scope: "session", Content: "missing session", Source: "user"},
		{Scope: "project", WorkspaceRoot: "D:/workspace", Source: "user"},
		{Scope: "project", Content: "agent missing trace", WorkspaceRoot: "D:/workspace", Source: "agent"},
	}
	for _, tc := range cases {
		if _, err := service.Create(tc); err == nil {
			t.Fatalf("expected create error for %#v", tc)
		}
	}

	created, err := service.Create(MemoryCreateRequest{
		Scope:         "project",
		Content:       "agent traced memory",
		WorkspaceRoot: "D:/workspace",
		Source:        "agent",
		RunID:         "run_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Source != "agent" || created.RunID != "run_1" {
		t.Fatalf("agent traced memory mismatch: %#v", created)
	}
}

func TestMemoryServiceRuntimeToolScopeAndMutation(t *testing.T) {
	_, service := newMemoryServiceTestFixture(t)
	currentProject, err := service.Create(MemoryCreateRequest{
		Scope:         "project",
		Content:       "Current project memory",
		WorkspaceRoot: "D:/workspace",
		Source:        "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	currentSession, err := service.Create(MemoryCreateRequest{
		Scope:     "session",
		Content:   "Current session memory",
		SessionID: "session_1",
		Source:    "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := service.Create(MemoryCreateRequest{
		Scope:     "session",
		Content:   "Other session memory",
		SessionID: "session_2",
		Source:    "user",
	})
	if err != nil {
		t.Fatal(err)
	}

	base := methods.MemoryToolExecuteParams{
		RunID:         "run_1",
		SessionID:     "session_1",
		WorkspaceRoot: "D:/workspace",
		ToolCallID:    "tool_memory",
	}
	list, err := service.ExecuteRuntimeTool(withMemoryTool(base, "memory.list", map[string]any{"limit": 10}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("list items = %#v, want current project and session only", list.Items)
	}
	for _, item := range list.Items {
		if item.ID == otherSession.ID {
			t.Fatalf("runtime list leaked other session memory: %#v", list.Items)
		}
	}

	created, err := service.ExecuteRuntimeTool(withMemoryTool(base, "memory.create", map[string]any{
		"scope":   "session",
		"content": "Agent-created memory",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if created.RecordID == "" || len(created.Items) != 1 || created.Items[0].Scope != "session" {
		t.Fatalf("created result mismatch: %#v", created)
	}
	row, err := service.repos.Memory.Get(created.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Source != "agent" || row.RunID != "run_1" || row.SessionID != "session_1" {
		t.Fatalf("created runtime memory trace mismatch: %#v", row)
	}

	updated, err := service.ExecuteRuntimeTool(withMemoryTool(base, "memory.update", map[string]any{
		"id":      currentProject.ID,
		"content": "Updated project memory",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Items[0].Content != "Updated project memory" {
		t.Fatalf("updated content mismatch: %#v", updated)
	}

	if _, err := service.ExecuteRuntimeTool(withMemoryTool(base, "memory.delete", map[string]any{"id": otherSession.ID})); err == nil {
		t.Fatal("expected delete of other session memory to fail")
	}
	if _, err := service.ExecuteRuntimeTool(withMemoryTool(base, "memory.update", map[string]any{"id": currentSession.ID, "scope": "project"})); err == nil {
		t.Fatal("expected scope-changing update to fail")
	}
}

func newMemoryServiceTestFixture(t *testing.T) (repository.Set, MemoryService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "memory-service.db")), &gorm.Config{})
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
	return repos, NewMemoryService(repos)
}

func ptrString(value string) *string {
	return &value
}

func withMemoryTool(base methods.MemoryToolExecuteParams, toolName string, args map[string]any) methods.MemoryToolExecuteParams {
	base.ToolName = toolName
	base.Arguments = args
	return base
}

func TestMemoryDTOHandlesBadMetadata(t *testing.T) {
	dto := memoryDTO(model.MemoryRecord{
		ID:           "mem_bad_metadata",
		Scope:        "project",
		Kind:         "fact",
		Status:       "active",
		Title:        "Bad metadata",
		Content:      "Bad metadata",
		Confidence:   "low",
		MetadataJSON: "{bad",
	})
	if dto.Metadata != nil {
		t.Fatalf("bad metadata should be hidden, got %#v", dto.Metadata)
	}
}
