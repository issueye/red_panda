package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	ptools "redpanda/protocol/tools"
)

// 标准工具结果架构版本。所有工具均应返回此封装，便于 UI、模型和恢复流程一致解析。
const toolResultSchemaV1 = "red_panda.tool_result.v1"

// maxToolResultForModel 限制每条回传给下一轮 LLM 的工具结果大小。
const maxToolResultForModel = 16 * 1024

// StandardToolResult 是工具成功或失败载荷的标准封装。
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

// ToolResultMeta 保存非载荷信息，如耗时和截断提示。
type ToolResultMeta struct {
	DurationMS    int64  `json:"duration_ms,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	OriginalBytes int    `json:"original_bytes,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Note          string `json:"note,omitempty"`
}

func StandardizeToolOutput(toolName string, raw string, runErr error, durationMS int64) string {
	if env, ok := ParseStandardToolResult(raw); ok {
		// 已是标准格式，仅在缺失时补充耗时。
		if env.Meta.DurationMS == 0 && durationMS > 0 {
			env.Meta.DurationMS = durationMS
		}
		if runErr != nil && env.Error == "" {
			env.Error = runErr.Error()
			env.OK = false
			env.Status = string(ptools.CallStatusFailed)
		}
		return mustMarshalToolResult(env)
	}

	if runErr != nil {
		errText := strings.TrimSpace(runErr.Error())
		return mustMarshalToolResult(StandardToolResult{
			Schema: toolResultSchemaV1,
			Tool:   toolName,
			Status: string(ptools.CallStatusFailed),
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
		text = PreferReadableText(data, raw)
	} else if raw != "" {
		// 纯文本已存入 Text；在 Data 中重复会使大型文件读取的 JSON-RPC 事件翻倍，
		// 从而超过扫描器或传输层限制。
		data = nil
	} else {
		data = map[string]any{}
		text = ""
	}

	originalBytes := len(raw)
	truncated := false
	if originalBytes > maxToolOutputBytes {
		// 将封装控制在存储和事件大小限制内，且必须通过元数据说明截断。
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
		Status: string(ptools.CallStatusCompleted),
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

func ParseStandardToolResult(raw string) (StandardToolResult, bool) {
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
		// 极少发生，仍提供可解析的回退结果。
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

// PreferReadableText 从结构化工具数据中提取人类可读的摘要。
func PreferReadableText(data any, fallback string) string {
	switch v := data.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "answer", "content", "output", "message", "summary"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		// web.search 风格的结果。
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
					b.WriteString(CompactOneLine(snippet, 200))
					b.WriteString("\n")
				}
			}
			if out := strings.TrimSpace(b.String()); out != "" {
				return out
			}
		}
		// 结构化数据的最后回退方式：格式化后的紧凑 JSON。
		if raw, err := json.Marshal(v); err == nil {
			return string(raw)
		}
	}
	return fallback
}

func CompactOneLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	// 避免在 UTF-8 字符中间截断。
	if max > 1 && utf8.ValidString(value[:max]) {
		return value[:max] + "…"
	}
	return string([]rune(value)[:max]) + "…"
}

// ModelFacingToolContent 返回供下一轮 LLM 使用的标准化、限长工具结果视图。
// 完整的 Result.Output 仍保留给 UI 和事件流。
func ModelFacingToolContent(result ptools.Result) string {
	env, ok := ParseStandardToolResult(result.Output)
	if !ok {
		// 旧版或非标准输出也要封装，确保模型始终收到统一架构。
		raw := strings.TrimSpace(result.Output)
		if raw == "" {
			raw = strings.TrimSpace(result.Error)
		}
		env = StandardToolResult{
			Schema: toolResultSchemaV1,
			Tool:   result.Name,
			Status: string(result.Status),
			OK:     result.Status == ptools.CallStatusCompleted,
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
		text = PreferReadableText(env.Data, "")
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
		// 数据较大时不将完整内容传给模型，但不能静默丢弃；
		// 改为指针说明，使模型知道 UI 仍保有完整载荷。
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
	// 优先在接近上限的位置按 UTF-8 字符安全截断。
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

// ReadableToolResultText 从标准工具结果中提取人类可读文本。
func ReadableToolResultText(result ptools.Result) string {
	if env, ok := ParseStandardToolResult(result.Output); ok {
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
		if strings.TrimSpace(env.Error) != "" {
			return strings.TrimSpace(env.Error)
		}
		if env.Data != nil {
			return PreferReadableText(env.Data, "")
		}
	}
	return strings.TrimSpace(result.Output)
}

// ExtractSearchAnswer 从 web.search 标准封装或原始 JSON 中提取答案和链接。
func ExtractSearchAnswer(raw string) string {
	if env, ok := ParseStandardToolResult(raw); ok {
		if m, ok := env.Data.(map[string]any); ok {
			if built := formatSearchDataLocal(m); built != "" {
				return built
			}
		}
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return formatSearchDataLocal(parsed)
}

func formatSearchDataLocal(v map[string]any) string {
	answer, _ := v["answer"].(string)
	items, _ := v["items"].([]any)
	if strings.TrimSpace(answer) == "" && len(items) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(answer) != "" {
		b.WriteString(strings.TrimSpace(answer))
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		url, _ := m["url"].(string)
		if strings.TrimSpace(url) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(url))
	}
	return b.String()
}
