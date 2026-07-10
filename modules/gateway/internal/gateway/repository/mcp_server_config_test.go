package repository

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
)

func TestMCPServerConfigRepositoryCRUDAndStructuredJSON(t *testing.T) {
	repo := newMCPServerConfigTestRepository(t)
	created, err := repo.Create(model.MCPServerConfig{
		Name:    "filesystem",
		Command: "mcp-filesystem",
		Args:    []string{"--root", "."},
		Env: map[string]string{
			"LOG_LEVEL": "warn",
		},
		CWD:     "D:/workspace",
		Enabled: true,
		Timeouts: model.MCPTimeouts{
			StartMS:      10000,
			InitializeMS: 10000,
			ListMS:       10000,
			CallMS:       30000,
			ShutdownMS:   3000,
		},
		ToolAllowlist: []string{"read_file", "list"},
		RiskOverrides: map[string]string{"read_file": "low"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("missing generated metadata: %#v", created)
	}

	loaded, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Args, created.Args) ||
		!reflect.DeepEqual(loaded.Env, created.Env) ||
		!reflect.DeepEqual(loaded.Timeouts, created.Timeouts) ||
		!reflect.DeepEqual(loaded.ToolAllowlist, created.ToolAllowlist) ||
		!reflect.DeepEqual(loaded.RiskOverrides, created.RiskOverrides) {
		t.Fatalf("structured fields did not round-trip: got %#v want %#v", loaded, created)
	}

	emptyArgs := []string{}
	emptyEnv := map[string]string{}
	emptyCWD := ""
	disabled := false
	updated, err := repo.Update(MCPServerConfigUpdate{
		ID:      created.ID,
		Args:    &emptyArgs,
		Env:     &emptyEnv,
		CWD:     &emptyCWD,
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled || updated.CWD != "" || len(updated.Args) != 0 || len(updated.Env) != 0 {
		t.Fatalf("update did not preserve explicit empty/false values: %#v", updated)
	}

	rows, err := repo.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != created.ID {
		t.Fatalf("list mismatch: %#v", rows)
	}

	if err := repo.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get after delete error = %v, want record not found", err)
	}
	rows, err = repo.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("deleted server remained in list: %#v", rows)
	}
}

func TestMCPServerConfigRepositoryUniqueActiveNameAndReuseAfterDelete(t *testing.T) {
	repo := newMCPServerConfigTestRepository(t)
	first, err := repo.Create(model.MCPServerConfig{
		Name:    "filesystem",
		Command: "mcp-filesystem",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(model.MCPServerConfig{
		Name:    first.Name,
		Command: "other-command",
	}); !errors.Is(err, ErrMCPServerNameExists) {
		t.Fatalf("duplicate create error = %v, want %v", err, ErrMCPServerNameExists)
	}

	second, err := repo.Create(model.MCPServerConfig{
		Name:    "github",
		Command: "mcp-github",
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicateName := first.Name
	if _, err := repo.Update(MCPServerConfigUpdate{
		ID:   second.ID,
		Name: &duplicateName,
	}); !errors.Is(err, ErrMCPServerNameExists) {
		t.Fatalf("duplicate update error = %v, want %v", err, ErrMCPServerNameExists)
	}

	if err := repo.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	recreated, err := repo.Create(model.MCPServerConfig{
		Name:    first.Name,
		Command: "replacement-command",
	})
	if err != nil {
		t.Fatal(err)
	}
	if recreated.ID == first.ID {
		t.Fatalf("recreated server reused deleted id %q", recreated.ID)
	}
	byName, err := repo.GetByName(first.Name)
	if err != nil {
		t.Fatal(err)
	}
	if byName.ID != recreated.ID {
		t.Fatalf("get by name = %#v, want recreated %#v", byName, recreated)
	}
}

func newMCPServerConfigTestRepository(t *testing.T) MCPServerConfigRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mcp.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&model.MCPServerConfig{}); err != nil {
		t.Fatal(err)
	}
	return NewMCPServerConfigRepository(db)
}
