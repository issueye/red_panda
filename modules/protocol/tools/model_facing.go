package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ResultSchemaV1 is the stable tool-result envelope version shared by Runtime,
// Gateway projections, and provider model context.
const ResultSchemaV1 = "red_panda.tool_result.v1"

const (
	// defaultResultForModel keeps ordinary tool results compact.
	defaultResultForModel = 16 * 1024
	// maxResultForModel is reserved for report-style tools whose content is the
	// actual delegated result rather than raw diagnostic output.
	maxResultForModel = 128 * 1024
)

type modelFacingEnvelope struct {
	Schema string          `json:"schema"`
	Tool   string          `json:"tool"`
	Status string          `json:"status"`
	OK     bool            `json:"ok"`
	Text   string          `json:"text,omitempty"`
	Data   any             `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
	Meta   modelFacingMeta `json:"meta"`
}

type modelFacingMeta struct {
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	OriginalBytes int    `json:"original_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Note          string `json:"note,omitempty"`
}

// ModelFacingContent returns the standardized, size-limited tool result view for
// the next provider turn. Full Result.Output remains for UI and event streams.
func ModelFacingContent(result Result) string {
	return ModelFacingContentWithLimit(result, defaultResultForModel)
}

// ModelFacingContentWithLimit returns the same stable envelope while applying
// a caller-provided total byte budget. Full Result.Output remains unchanged.
func ModelFacingContentWithLimit(result Result, maxBytes int) string {
	if maxBytes <= 0 || maxBytes > maxResultForModel {
		maxBytes = maxResultForModel
	}
	env, ok := parseModelFacingEnvelope(result.Output)
	if !ok {
		raw := strings.TrimSpace(result.Output)
		if raw == "" {
			raw = strings.TrimSpace(result.Error)
		}
		env = modelFacingEnvelope{
			Schema: ResultSchemaV1,
			Tool:   result.Name,
			Status: string(result.Status),
			OK:     result.Status == CallStatusCompleted,
			Text:   raw,
			Error:  strings.TrimSpace(result.Error),
			Meta: modelFacingMeta{
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
	env.Text = text
	if original > 0 {
		env.Meta.OriginalBytes = original
	}

	if len(mustMarshalModelFacing(env)) > maxBytes && env.Data != nil {
		env.Data = nil
		truncated = true
	}
	if truncated && env.Meta.Note == "" {
		env.Meta.Note = fmt.Sprintf("payload truncated for model context; original_bytes=%d", original)
	}
	env.Meta.Truncated = truncated

	for {
		raw := mustMarshalModelFacing(env)
		if len(raw) <= maxBytes || env.Text == "" {
			return raw
		}
		overflow := len(raw) - maxBytes
		nextBytes := len(env.Text) - overflow - 1
		if nextBytes <= 0 {
			env.Text = ""
		} else {
			env.Text = trimToBytes(env.Text, nextBytes)
		}
		env.Meta.Truncated = true
		if env.Meta.Note == "" {
			env.Meta.Note = fmt.Sprintf("payload truncated for model context; original_bytes=%d", original)
		}
	}
}

func parseModelFacingEnvelope(raw string) (modelFacingEnvelope, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return modelFacingEnvelope{}, false
	}
	var env modelFacingEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return modelFacingEnvelope{}, false
	}
	if env.Schema != ResultSchemaV1 || env.Tool == "" {
		return modelFacingEnvelope{}, false
	}
	return env, true
}

func mustMarshalModelFacing(env modelFacingEnvelope) string {
	if env.Schema == "" {
		env.Schema = ResultSchemaV1
	}
	raw, err := json.Marshal(env)
	if err != nil {
		fallback, _ := json.Marshal(map[string]any{
			"schema": ResultSchemaV1,
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
	if max > 1 && utf8.ValidString(value[:max]) {
		return value[:max] + "…"
	}
	return string([]rune(value)[:max]) + "…"
}

func trimToBytes(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	const ellipsis = "…"
	if max <= len(ellipsis) {
		return value[:max]
	}
	cut := max - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	if cut <= 0 {
		return value[:max]
	}
	return value[:cut] + ellipsis
}
