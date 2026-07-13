package repository

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/events"
	"redpanda/protocol/permission"
)

func TestPermissionRequestRepositoryProjectAndResolve(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "permissions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.PermissionRequest{}); err != nil {
		t.Fatal(err)
	}

	repo := NewPermissionRequestRepository(db)
	event := events.EnvelopeV2{
		EventID:      "evt_1",
		RunID:        "run_1",
		SessionID:    "session_1",
		AssignmentID: "assignment_1",
		RunSeq:       3,
		Type:         events.EventPermissionRequest,
		Payload: map[string]any{
			"permission_id": "perm_1",
			"run_id":        "run_1",
			"tool_call_id":  "tool_1",
			"tool_name":     "shell.exec",
			"risk":          "high",
			"summary":       "Allow shell",
			"detail":        "Run shell command",
			"arguments":     map[string]any{"command": "echo ok"},
		},
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.ProjectRequired(event); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "perm_1" || pending[0].Status != "pending" {
		t.Fatalf("pending projection mismatch: %#v", pending)
	}
	if err := repo.Resolve(permission.ResolveParams{
		PermissionID: "perm_1",
		Decision:     permission.DecisionApprove,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.Get("perm_1")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "resolved" || row.Decision != "approve" {
		t.Fatalf("resolve mismatch: %#v", row)
	}
}
