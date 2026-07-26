package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const maxLLMLogBodyBytes = 4 << 20 // 4 MiB per request file

var llmLogMu sync.Mutex

// defaultLLMLogDir 是可选 LLM 请求诊断日志的目录。
// 可通过 RED_PANDA_LOG_DIR 覆盖，其下会创建 llm-requests 目录。
func defaultLLMLogDir() string {
	if dir := strings.TrimSpace(os.Getenv("RED_PANDA_LOG_DIR")); dir != "" {
		return filepath.Join(dir, "llm-requests")
	}
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, "red-panda", "logs", "llm-requests")
	}
	return filepath.Join(os.TempDir(), "red-panda", "logs", "llm-requests")
}

// logLLMRequest 在启用日志时写入发往提供方的 JSON 请求体。
// API 密钥仅存在于 HTTP 标头中，绝不写入日志。
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

	// Redact image base64 payloads before any truncation / write so vision
	// requests never leak pixel data into llm-requests logs (docs/51 §9).
	sanitized := redactImagePayloads(rawBody)
	body := sanitized
	truncated := false
	if len(body) > maxLLMLogBodyBytes {
		body = body[:maxLLMLogBodyBytes]
		truncated = true
	}

	// 尽可能格式化输出，便于离线检查文件。
	var pretty json.RawMessage
	payload := map[string]any{
		"logged_at":  time.Now().UTC().Format(time.RFC3339Nano),
		"run_id":     runID,
		"session_id": sessionID,
		"model":      model,
		"url":        url,
		"body_bytes": len(rawBody),
		"truncated":  truncated,
		"note":       "API keys are never written to this log. Image base64 payloads are redacted. Enable via Desktop settings → 记录 LLM 请求.",
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

// redactImagePayloads walks a JSON request body and replaces base64 image
// payloads with short placeholders. Falls back to a data-URL strip when the
// body is not JSON.
func redactImagePayloads(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return []byte(stripDataURLs(string(raw)))
	}
	redactValue(root)
	out, err := json.Marshal(root)
	if err != nil {
		return []byte(stripDataURLs(string(raw)))
	}
	return out
}

func redactValue(v any) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			key := strings.ToLower(k)
			switch key {
			case "data", "data_b64", "b64_json":
				if s, ok := child.(string); ok && looksLikeBase64(s) {
					node[k] = fmt.Sprintf("[redacted base64 %d chars]", len(s))
					continue
				}
			case "url":
				if s, ok := child.(string); ok {
					node[k] = stripDataURLs(s)
					continue
				}
			case "image_url":
				switch c := child.(type) {
				case string:
					node[k] = stripDataURLs(c)
					continue
				case map[string]any:
					if u, ok := c["url"].(string); ok {
						c["url"] = stripDataURLs(u)
					}
				}
			case "source":
				if m, ok := child.(map[string]any); ok {
					if s, ok := m["data"].(string); ok && looksLikeBase64(s) {
						m["data"] = fmt.Sprintf("[redacted base64 %d chars]", len(s))
					}
				}
			}
			redactValue(child)
		}
	case []any:
		for _, child := range node {
			redactValue(child)
		}
	}
}

func looksLikeBase64(s string) bool {
	if len(s) < 64 {
		return false
	}
	// Heuristic: long strings without spaces that are mostly base64 alphabet.
	limit := len(s)
	if limit > 128 {
		limit = 128
	}
	for i := 0; i < limit; i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r' {
			continue
		}
		return false
	}
	return true
}

func stripDataURLs(s string) string {
	const marker = "data:"
	const b64mark = ";base64,"
	out := s
	for {
		idx := strings.Index(out, marker)
		if idx < 0 {
			return out
		}
		rest := out[idx:]
		b64 := strings.Index(rest, b64mark)
		if b64 < 0 {
			return out
		}
		start := b64 + len(b64mark)
		j := start
		for j < len(rest) {
			c := rest[j]
			if c == '"' || c == '\'' || unicode.IsSpace(rune(c)) || c == '}' || c == ',' {
				break
			}
			j++
		}
		placeholder := fmt.Sprintf("data:[redacted base64 %d chars]", j-start)
		out = out[:idx] + placeholder + rest[j:]
	}
}
