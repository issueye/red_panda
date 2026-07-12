package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"redpanda/protocol/tools"
)

// Standard tool result schema version. All tools should return this envelope so
// UI, model, and recovery paths can parse results consistently.
const toolResultSchemaV1 = "red_panda.tool_result.v1"

// StandardToolResult is the canonical envelope for tool success/failure payloads.
type StandardToolResult struct {
	Schema string         `json:"schema"`
	Tool   string         `json:"tool"`
	Status string         `json:"status"`
	OK     bool           `json:"ok"`
	Text   string         `json:"text,omitempty"`
	Data   any            `json:"data,omitempty"`
	Error  string         `json:"error,omitempty"`
	Meta   ToolResultMeta `json:"meta"`
}

// ToolResultMeta carries non-payload facts (timing, truncation notices, etc.).
type ToolResultMeta struct {
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	OriginalBytes int    `json:"original_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Note          string `json:"note,omitempty"`
}

func standardizeToolOutput(toolName string, raw string, runErr error, durationMS int64) string {
	if env, ok := parseStandardToolResult(raw); ok {
		// Already standardized — only refresh duration when missing.
		if env.Meta.DurationMS == 0 && durationMS > 0 {
			env.Meta.DurationMS = durationMS
		}
		if runErr != nil && env.Error == "" {
			env.Error = runErr.Error()
			env.OK = false
			env.Status = string(tools.CallStatusFailed)
		}
		return mustMarshalToolResult(env)
	}

	if runErr != nil {
		errText := strings.TrimSpace(runErr.Error())
		return mustMarshalToolResult(StandardToolResult{
			Schema: toolResultSchemaV1,
			Tool:   toolName,
			Status: string(tools.CallStatusFailed),
			OK:     false,
			Text:   errText,
			Error:  errText,
			Data: map[string]any{
				"raw": strings.TrimSpace(raw),
			},
			Meta: ToolResultMeta{
				DurationMS: durationMS,
				Note:       "standardized failure envelope",
			},
		})
	}

	raw = strings.TrimSpace(raw)
	var data any
	text := raw
	if raw != "" && json.Unmarshal([]byte(raw), &data) == nil {
		text = preferReadableText(data, raw)
	} else if raw != "" {
		// Plain text already lives in Text. Duplicating it in Data can double a
		// JSON-RPC event beyond scanner/transport limits for large file reads.
		data = nil
	} else {
		data = map[string]any{}
		text = ""
	}

	originalBytes := len(raw)
	truncated := false
	if originalBytes > maxToolOutputBytes {
		// Keep envelope under storage/event size; never silently drop without meta.
		if s, ok := data.(string); ok && len(s) > maxToolOutputBytes {
			data = s[:maxToolOutputBytes]
		}
		if len(text) > maxToolOutputBytes {
			text = text[:maxToolOutputBytes] + "\n…[truncated]"
		}
		truncated = true
	}

	return mustMarshalToolResult(StandardToolResult{
		Schema: toolResultSchemaV1,
		Tool:   toolName,
		Status: string(tools.CallStatusCompleted),
		OK:     true,
		Text:   text,
		Data:   data,
		Meta: ToolResultMeta{
			DurationMS:    durationMS,
			Truncated:     truncated,
			OriginalBytes: originalBytes,
			Note:          truncationNote(truncated, originalBytes),
		},
	})
}

func truncationNote(truncated bool, originalBytes int) string {
	if !truncated {
		return ""
	}
	return fmt.Sprintf("payload truncated for transport; original_bytes=%d", originalBytes)
}

func parseStandardToolResult(raw string) (StandardToolResult, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return StandardToolResult{}, false
	}
	var env StandardToolResult
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return StandardToolResult{}, false
	}
	if env.Schema != toolResultSchemaV1 {
		return StandardToolResult{}, false
	}
	if env.Tool == "" {
		return StandardToolResult{}, false
	}
	return env, true
}

func mustMarshalToolResult(env StandardToolResult) string {
	if env.Schema == "" {
		env.Schema = toolResultSchemaV1
	}
	raw, err := json.Marshal(env)
	if err != nil {
		// Extremely unlikely; keep a parseable fallback.
		fallback, _ := json.Marshal(map[string]any{
			"schema": toolResultSchemaV1,
			"tool":   env.Tool,
			"status": env.Status,
			"ok":     env.OK,
			"text":   env.Text,
			"error":  env.Error,
			"meta":   map[string]any{"note": "marshal_fallback"},
		})
		return string(fallback)
	}
	return string(raw)
}

// preferReadableText extracts a human-readable summary from structured tool data.
func preferReadableText(data any, fallback string) string {
	switch v := data.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "answer", "content", "output", "message", "summary"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		// web.search style
		if items, ok := v["items"].([]any); ok && len(items) > 0 {
			var b strings.Builder
			if answer, ok := v["answer"].(string); ok && strings.TrimSpace(answer) != "" {
				b.WriteString(strings.TrimSpace(answer))
				b.WriteString("\n")
			}
			limit := len(items)
			if limit > 8 {
				limit = 8
			}
			for i := 0; i < limit; i++ {
				item, ok := items[i].(map[string]any)
				if !ok {
					continue
				}
				title, _ := item["title"].(string)
				url, _ := item["url"].(string)
				snippet, _ := item["snippet"].(string)
				if title == "" && url == "" {
					continue
				}
				b.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(title)))
				if url != "" {
					b.WriteString("   ")
					b.WriteString(url)
					b.WriteString("\n")
				}
				if snippet != "" {
					b.WriteString("   ")
					b.WriteString(compactOneLine(snippet, 200))
					b.WriteString("\n")
				}
			}
			if out := strings.TrimSpace(b.String()); out != "" {
				return out
			}
		}
		// Pretty compact JSON as last resort for structured data.
		if raw, err := json.Marshal(v); err == nil {
			return string(raw)
		}
	}
	return fallback
}

func compactOneLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	// Avoid cutting mid-rune.
	if max > 1 && utf8.ValidString(value[:max]) {
		return value[:max] + "…"
	}
	return string([]rune(value)[:max]) + "…"
}

// modelFacingToolContent returns a standardized, bounded view of a tool result
// for the next LLM turn. Full Result.Output is preserved for UI/events.
func modelFacingToolContent(result tools.Result) string {
	env, ok := parseStandardToolResult(result.Output)
	if !ok {
		// Legacy/non-standard output — still wrap so the model always sees a schema.
		raw := strings.TrimSpace(result.Output)
		if raw == "" {
			raw = strings.TrimSpace(result.Error)
		}
		env = StandardToolResult{
			Schema: toolResultSchemaV1,
			Tool:   result.Name,
			Status: string(result.Status),
			OK:     result.Status == tools.CallStatusCompleted,
			Text:   raw,
			Error:  strings.TrimSpace(result.Error),
			Data:   map[string]any{"content": raw},
			Meta: ToolResultMeta{
				DurationMS: result.DurationMS,
				Note:       "legacy output wrapped for model",
			},
		}
	}

	text := strings.TrimSpace(env.Text)
	if text == "" {
		text = strings.TrimSpace(env.Error)
	}
	if text == "" && env.Data != nil {
		text = preferReadableText(env.Data, "")
	}

	original := len(text)
	truncated := env.Meta.Truncated
	if original > maxToolResultForModel {
		text = trimToBytes(text, maxToolResultForModel)
		truncated = true
	}
	env.Text = text
	env.Meta.Truncated = truncated
	if original > 0 {
		env.Meta.OriginalBytes = original
	}
	if truncated {
		if env.Meta.Note == "" {
			env.Meta.Note = truncationNote(true, original)
		}
		// Keep full data out of the model view when large — but never silently drop:
		// replace with a pointer note so the model knows UI has the full payload.
		if env.Data != nil {
			env.Data = map[string]any{
				"omitted":        true,
				"reason":         "size_limit_for_model_context",
				"original_bytes": original,
				"full_output_in": "tool_finished.event / UI tool card",
				"preview":        text,
			}
		}
	}
	return mustMarshalToolResult(env)
}

func trimToBytes(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	// Prefer rune-safe cut near max.
	if max < 4 {
		return value[:max]
	}
	cut := max - 1
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	if cut <= 0 {
		cut = max
	}
	return value[:cut] + "…"
}
