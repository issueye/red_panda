package service

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	protocolmcp "redpanda/protocol/mcp"
)

func TestMCPServerConfigServiceCRUDDefaultsAndEnvRedaction(t *testing.T) {
	repos, service := newMCPServerConfigServiceFixture(t)
	created, err := service.Create(protocolmcp.MCPServerConfig{
		Name:    "filesystem",
		Command: "not-installed-mcp-test-command",
		Args:    []string{"--root", "."},
		Env: map[string]string{
			"LOG_LEVEL": "warn",
			"api_token": "secret-token-value",
		},
		Enabled:       true,
		ToolAllowlist: []string{"read_file", "list"},
		RiskOverrides: map[string]string{"read_file": "low"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "filesystem" || !created.Enabled {
		t.Fatalf("unexpected create response: %#v", created)
	}
	if !reflect.DeepEqual(created.Timeouts, protocolmcp.DefaultTimeouts()) {
		t.Fatalf("timeouts = %#v, want defaults %#v", created.Timeouts, protocolmcp.DefaultTimeouts())
	}
	assertMCPEnvRedacted(t, created.Env)

	stored, err := repos.MCPServers.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Env["api_token"] != "secret-token-value" {
		t.Fatalf("stored secret was changed: %#v", stored.Env)
	}

	disabled := false
	updated, err := service.Update(created.ID, MCPServerConfigUpdate{Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatalf("enabled = true, want false: %#v", updated)
	}
	assertMCPEnvRedacted(t, updated.Env)
	stored, err = repos.MCPServers.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Env["api_token"] != "secret-token-value" {
		t.Fatalf("partial update did not preserve env: %#v", stored.Env)
	}

	got, err := service.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertMCPEnvRedacted(t, got.Env)
	listed, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Servers) != 1 || listed.Servers[0].ID != created.ID {
		t.Fatalf("unexpected list response: %#v", listed)
	}
	assertMCPEnvRedacted(t, listed.Servers[0].Env)

	if err := service.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get after delete error = %v, want record not found", err)
	}
}

func TestMCPServerConfigServiceRejectsInvalidConfig(t *testing.T) {
	_, service := newMCPServerConfigServiceFixture(t)
	tests := []struct {
		name   string
		mutate func(*protocolmcp.MCPServerConfig)
	}{
		{"missing name", func(config *protocolmcp.MCPServerConfig) { config.Name = "" }},
		{"uppercase name", func(config *protocolmcp.MCPServerConfig) { config.Name = "FileSystem" }},
		{"name whitespace", func(config *protocolmcp.MCPServerConfig) { config.Name = "file system" }},
		{"name double underscore", func(config *protocolmcp.MCPServerConfig) { config.Name = "file__system" }},
		{"name slash", func(config *protocolmcp.MCPServerConfig) { config.Name = "file/system" }},
		{"name backslash", func(config *protocolmcp.MCPServerConfig) { config.Name = `file\system` }},
		{"name shell syntax", func(config *protocolmcp.MCPServerConfig) { config.Name = "file;system" }},
		{"name non ascii", func(config *protocolmcp.MCPServerConfig) { config.Name = "文件" }},
		{"missing command", func(config *protocolmcp.MCPServerConfig) { config.Command = "" }},
		{"command line", func(config *protocolmcp.MCPServerConfig) { config.Command = "node server.js" }},
		{"command shell syntax", func(config *protocolmcp.MCPServerConfig) { config.Command = "server;remove" }},
		{"empty env key", func(config *protocolmcp.MCPServerConfig) { config.Env = map[string]string{" ": "value"} }},
		{"negative start timeout", func(config *protocolmcp.MCPServerConfig) { config.Timeouts.StartMS = -1 }},
		{"negative initialize timeout", func(config *protocolmcp.MCPServerConfig) { config.Timeouts.InitializeMS = -1 }},
		{"negative list timeout", func(config *protocolmcp.MCPServerConfig) { config.Timeouts.ListMS = -1 }},
		{"negative call timeout", func(config *protocolmcp.MCPServerConfig) { config.Timeouts.CallMS = -1 }},
		{"negative shutdown timeout", func(config *protocolmcp.MCPServerConfig) { config.Timeouts.ShutdownMS = -1 }},
		{"empty allowlist entry", func(config *protocolmcp.MCPServerConfig) { config.ToolAllowlist = []string{"read", " "} }},
		{"empty risk tool", func(config *protocolmcp.MCPServerConfig) { config.RiskOverrides = map[string]string{"": "low"} }},
		{"invalid risk", func(config *protocolmcp.MCPServerConfig) { config.RiskOverrides = map[string]string{"read": "trusted"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validMCPServerConfig()
			test.mutate(&config)
			if _, err := service.Create(config); !errors.Is(err, ErrInvalidMCPServerConfig) {
				t.Fatalf("Create() error = %v, want invalid config", err)
			}
		})
	}
}

func TestMCPServerConfigServiceRejectsDiscoveryForDisabledServer(t *testing.T) {
	_, service := newMCPServerConfigServiceFixture(t)
	created, err := service.Create(protocolmcp.MCPServerConfig{
		Name:    "disabled",
		Command: "mcp-disabled",
		Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Discover(context.Background(), created.ID, "")
	if !errors.Is(err, ErrInvalidMCPServerConfig) {
		t.Fatalf("Discover error = %v, want invalid config", err)
	}
}

func TestMCPServerConfigServiceRejectsDuplicateName(t *testing.T) {
	_, service := newMCPServerConfigServiceFixture(t)
	config := validMCPServerConfig()
	if _, err := service.Create(config); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(config); !errors.Is(err, repository.ErrMCPServerNameExists) {
		t.Fatalf("duplicate error = %v, want %v", err, repository.ErrMCPServerNameExists)
	}
}

func validMCPServerConfig() protocolmcp.MCPServerConfig {
	return protocolmcp.MCPServerConfig{
		Name:          "filesystem",
		Command:       "mcp-filesystem",
		Args:          []string{"--root", "."},
		Env:           map[string]string{"LOG_LEVEL": "warn"},
		CWD:           "workspace",
		Enabled:       true,
		ToolAllowlist: []string{"read_file"},
		RiskOverrides: map[string]string{"read_file": "low"},
	}
}

func assertMCPEnvRedacted(t *testing.T, env map[string]string) {
	t.Helper()
	if env["api_token"] != redactedMCPEnvValue {
		t.Fatalf("sensitive env was not redacted: %#v", env)
	}
	if env["LOG_LEVEL"] != "warn" {
		t.Fatalf("non-sensitive env was unexpectedly redacted: %#v", env)
	}
}

func newMCPServerConfigServiceFixture(t *testing.T) (repository.Set, MCPServerConfigService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mcp-service.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.MCPServerConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewSet(db)
	return repos, NewMCPServerConfigService(repos)
}
