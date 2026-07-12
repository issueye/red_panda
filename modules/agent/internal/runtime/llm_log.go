package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxLLMLogBodyBytes = 4 << 20 // 4 MiB per request file

var llmLogMu sync.Mutex

// defaultLLMLogDir is the directory for optional LLM request diagnostics.
// Override with RED_PANDA_LOG_DIR (llm-requests is created underneath).
func defaultLLMLogDir() string {
	if dir := strings.TrimSpace(os.Getenv("RED_PANDA_LOG_DIR")); dir != "" {
		return filepath.Join(dir, "llm-requests")
	}
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, "red-panda", "logs", "llm-requests")
	}
	return filepath.Join(os.TempDir(), "red-panda", "logs", "llm-requests")
}

// logLLMRequest writes the outbound provider JSON body when logging is enabled.
// API keys live only in HTTP headers and are never written.
func logLLMRequest(enabled bool, runID string, sessionID string, model string, url string, rawBody []byte) (string, error) {
	if !enabled {
		return "", nil
	}
	llmLogMu.Lock()
	defer llmLogMu.Unlock()

	dir := defaultLLMLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create llm log dir: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102T150405.000Z")
	safeRun := sanitizeLogToken(runID)
	if safeRun == "" {
		safeRun = "run"
	}
	name := fmt.Sprintf("%s_%s.json", stamp, safeRun)
	path := filepath.Join(dir, name)

	body := rawBody
	truncated := false
	if len(body) > maxLLMLogBodyBytes {
		body = body[:maxLLMLogBodyBytes]
		truncated = true
	}

	// Pretty-print when possible so files are easy to inspect offline.
	var pretty json.RawMessage
	payload := map[string]any{
		"logged_at":   time.Now().UTC().Format(time.RFC3339Nano),
		"run_id":      runID,
		"session_id":  sessionID,
		"model":       model,
		"url":         url,
		"body_bytes":  len(rawBody),
		"truncated":   truncated,
		"note":        "API keys are never written to this log. Enable via Desktop settings → 记录 LLM 请求.",
	}
	if json.Valid(body) {
		pretty = json.RawMessage(body)
		payload["request"] = pretty
	} else {
		payload["request_raw"] = string(body)
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func sanitizeLogToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}
