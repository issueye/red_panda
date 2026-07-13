package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogLLMRequestDisabledIsNoop(t *testing.T) {
	path, err := logLLMRequest(false, "run1", "sess1", "model", "https://example/v1/chat/completions", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("expected empty path when disabled, got %q", path)
	}
}

func TestLogLLMRequestWritesPrettyJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RED_PANDA_LOG_DIR", dir)

	body := []byte(`{"model":"demo","messages":[{"role":"user","content":"hi"}]}`)
	path, err := logLLMRequest(true, "run_abc-1", "session_1", "demo", "https://provider.example/v1/chat/completions", body)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected log path")
	}
	if !strings.HasPrefix(path, filepath.Join(dir, "llm-requests")) {
		t.Fatalf("path %q not under log dir", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") {
		t.Fatal("log must never contain API key material")
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid log json: %v\n%s", err, raw)
	}
	if parsed["run_id"] != "run_abc-1" {
		t.Fatalf("run_id = %#v", parsed["run_id"])
	}
	if parsed["request"] == nil {
		t.Fatalf("missing request field: %s", raw)
	}
}

func TestDefaultLLMLogDirUsesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RED_PANDA_LOG_DIR", dir)
	got := defaultLLMLogDir()
	want := filepath.Join(dir, "llm-requests")
	if got != want {
		t.Fatalf("defaultLLMLogDir = %q, want %q", got, want)
	}
}
