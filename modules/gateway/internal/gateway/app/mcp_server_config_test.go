package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/controller"
	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/gateway/internal/gateway/service"
	protocolmcp "redpanda/protocol/mcp"
)

type mcpAPIEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestMCPServerConfigRoutesCRUDValidationAndRedaction(t *testing.T) {
	router, repos := newMCPServerConfigRouter(t)
	createBody := map[string]any{
		"name":    "filesystem",
		"command": "not-installed-mcp-test-command",
		"args":    []string{"--root", "."},
		"env": map[string]string{
			"LOG_LEVEL": "warn",
			"API_TOKEN": "top-secret-token",
		},
		"enabled":        true,
		"tool_allowlist": []string{"read_file"},
		"risk_overrides": map[string]string{"read_file": "low"},
	}

	createdEnvelope := performMCPRequest(t, router, http.MethodPost, "/api/v1/mcp/servers", createBody, http.StatusOK)
	var created protocolmcp.MCPServerResponse
	decodeMCPData(t, createdEnvelope, &created)
	if created.ID == "" || created.Command != "not-installed-mcp-test-command" {
		t.Fatalf("unexpected created server: %#v", created)
	}
	assertMCPAPIEnv(t, created.Env)
	stored, err := repos.MCPServers.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Env["API_TOKEN"] != "top-secret-token" {
		t.Fatalf("stored env was not preserved: %#v", stored.Env)
	}

	gotEnvelope := performMCPRequest(t, router, http.MethodGet, "/api/v1/mcp/servers/"+created.ID, nil, http.StatusOK)
	var got protocolmcp.MCPServerResponse
	decodeMCPData(t, gotEnvelope, &got)
	assertMCPAPIEnv(t, got.Env)

	listEnvelope := performMCPRequest(t, router, http.MethodGet, "/api/v1/mcp/servers", nil, http.StatusOK)
	var listed protocolmcp.MCPServerListResponse
	decodeMCPData(t, listEnvelope, &listed)
	if len(listed.Servers) != 1 || listed.Servers[0].ID != created.ID {
		t.Fatalf("unexpected list response: %#v", listed)
	}
	assertMCPAPIEnv(t, listed.Servers[0].Env)

	updatedEnvelope := performMCPRequest(t, router, http.MethodPut, "/api/v1/mcp/servers/"+created.ID, map[string]any{"enabled": false}, http.StatusOK)
	var updated protocolmcp.MCPServerResponse
	decodeMCPData(t, updatedEnvelope, &updated)
	if updated.Enabled || updated.Name != created.Name || updated.Command != created.Command {
		t.Fatalf("partial update lost config: %#v", updated)
	}
	assertMCPAPIEnv(t, updated.Env)
	discovery := performMCPRequest(t, router, http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/discover", map[string]any{}, http.StatusBadRequest)
	if discovery.Error == nil || discovery.Error.Code != "invalid_mcp_server_config" {
		t.Fatalf("unexpected disabled discovery error: %#v", discovery)
	}

	invalid := performMCPRequest(t, router, http.MethodPost, "/api/v1/mcp/servers", map[string]any{
		"name": "Invalid Name", "command": "mcp-invalid",
	}, http.StatusBadRequest)
	if invalid.Error == nil || invalid.Error.Code != "invalid_mcp_server_config" {
		t.Fatalf("unexpected validation error: %#v", invalid)
	}
	duplicate := performMCPRequest(t, router, http.MethodPost, "/api/v1/mcp/servers", createBody, http.StatusConflict)
	if duplicate.Error == nil || duplicate.Error.Code != "mcp_server_name_exists" {
		t.Fatalf("unexpected duplicate error: %#v", duplicate)
	}

	deletedEnvelope := performMCPRequest(t, router, http.MethodDelete, "/api/v1/mcp/servers/"+created.ID, nil, http.StatusOK)
	var deleted protocolmcp.MCPServerDeleteResponse
	decodeMCPData(t, deletedEnvelope, &deleted)
	if !deleted.Deleted || deleted.ID != created.ID {
		t.Fatalf("unexpected delete response: %#v", deleted)
	}
	missing := performMCPRequest(t, router, http.MethodGet, "/api/v1/mcp/servers/"+created.ID, nil, http.StatusNotFound)
	if missing.Error == nil || missing.Error.Code != "mcp_server_not_found" {
		t.Fatalf("unexpected not found error: %#v", missing)
	}
}

func newMCPServerConfigRouter(t *testing.T) (http.Handler, repository.Set) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mcp-api.db")), &gorm.Config{})
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
	hub := eventhub.New()
	services := service.NewSet(service.Options{Repos: repos, Hub: hub})
	controllers := controller.NewSet(services, hub)
	return NewRouter(Config{}, controllers), repos
}

func performMCPRequest(t *testing.T, router http.Handler, method string, path string, body any, wantStatus int) mcpAPIEnvelope {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, response.Code, wantStatus, response.Body.String())
	}
	var envelope mcpAPIEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func decodeMCPData(t *testing.T, envelope mcpAPIEnvelope, target any) {
	t.Helper()
	if !envelope.OK {
		t.Fatalf("request failed: %#v", envelope.Error)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatal(err)
	}
}

func assertMCPAPIEnv(t *testing.T, env map[string]string) {
	t.Helper()
	if env["API_TOKEN"] != "****" || env["LOG_LEVEL"] != "warn" {
		t.Fatalf("unexpected env response: %#v", env)
	}
}
