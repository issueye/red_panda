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
)

func TestWorkerProfileRoutesCRUDAndEnabledList(t *testing.T) {
	router := newWorkerProfileRouter(t)
	created := performWorkerProfileRequest(t, router, http.MethodPost, "/api/v1/worker-profiles", map[string]any{
		"key": "api-worker", "name": "API Worker", "provider": "openai", "model": "gpt-5",
	}, http.StatusOK)
	var profile service.WorkerProfileDTO
	decodeWorkerProfileData(t, created, &profile)
	if profile.ID == "" || profile.Provider != "openai" || profile.Model != "gpt-5" {
		t.Fatalf("unexpected create: %#v", profile)
	}

	got := performWorkerProfileRequest(t, router, http.MethodGet, "/api/v1/worker-profiles/"+profile.ID, nil, http.StatusOK)
	var fetched service.WorkerProfileDTO
	decodeWorkerProfileData(t, got, &fetched)
	if fetched.ID != profile.ID {
		t.Fatalf("unexpected get: %#v", fetched)
	}

	updated := performWorkerProfileRequest(t, router, http.MethodPut, "/api/v1/worker-profiles/"+profile.ID, map[string]any{
		"provider": "local", "model": "qwen3", "enabled": false,
	}, http.StatusOK)
	decodeWorkerProfileData(t, updated, &profile)
	if profile.Provider != "local" || profile.Model != "qwen3" || profile.Enabled {
		t.Fatalf("unexpected update: %#v", profile)
	}

	enabled := performWorkerProfileRequest(t, router, http.MethodGet, "/api/v1/worker-profiles/enabled", nil, http.StatusOK)
	var enabledProfiles []service.WorkerProfileDTO
	decodeWorkerProfileData(t, enabled, &enabledProfiles)
	for _, item := range enabledProfiles {
		if item.ID == profile.ID {
			t.Fatal("disabled profile returned by enabled route")
		}
	}

	listed := performWorkerProfileRequest(t, router, http.MethodGet, "/api/v1/worker-profiles", nil, http.StatusOK)
	var profiles []service.WorkerProfileDTO
	decodeWorkerProfileData(t, listed, &profiles)
	if len(profiles) < 6 {
		t.Fatalf("list returned %d profiles, want custom plus builtins", len(profiles))
	}

	performWorkerProfileRequest(t, router, http.MethodDelete, "/api/v1/worker-profiles/"+profile.ID, nil, http.StatusOK)
	performWorkerProfileRequest(t, router, http.MethodGet, "/api/v1/worker-profiles/"+profile.ID, nil, http.StatusNotFound)
}

type workerProfileAPIEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func newWorkerProfileRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "worker-profile-api.db")), &gorm.Config{})
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
	hub := eventhub.New()
	services := service.NewSet(service.Options{Repos: repos, Hub: hub})
	return NewRouter(Config{}, controller.NewSet(services, hub))
}

func performWorkerProfileRequest(t *testing.T, router http.Handler, method, path string, body any, wantStatus int) workerProfileAPIEnvelope {
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
	var envelope workerProfileAPIEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func decodeWorkerProfileData(t *testing.T, envelope workerProfileAPIEnvelope, target any) {
	t.Helper()
	if !envelope.OK {
		t.Fatalf("request failed: %#v", envelope.Error)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatal(err)
	}
}
