package provider

import (
	"fmt"
	"strings"
)

// openAICompatibleContent maps Message.Content to the OpenAI chat-completions
// "content" field: a string for text-only turns, or an array of parts when the
// turn includes images (docs/51 §6.6).
func openAICompatibleContent(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []Part:
		if len(v) == 0 {
			return ""
		}
		// Pure-text []Part collapses to string so golden JSON / simple models
		// stay on the historical wire shape.
		if allTextParts(v) {
			return joinPartTexts(v)
		}
		out := make([]map[string]any, 0, len(v))
		for _, p := range v {
			switch {
			case p.Type == "image_url" || p.ImageURL != nil:
				url := ""
				if p.ImageURL != nil {
					url = p.ImageURL.URL
				}
				out = append(out, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": url},
				})
			case p.Type == "image" && p.Source != nil:
				// Accept Anthropic-shaped Parts and rewrite for OpenAI.
				url := fmt.Sprintf("data:%s;base64,%s", p.Source.MediaType, p.Source.Data)
				out = append(out, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": url},
				})
			default:
				text := p.Text
				if text == "" && p.Type != "text" && p.Type != "input_text" {
					continue
				}
				out = append(out, map[string]any{"type": "text", "text": text})
			}
		}
		if len(out) == 0 {
			return ""
		}
		return out
	default:
		return fmt.Sprint(v)
	}
}

// anthropicContent maps Message.Content for the Anthropic Messages API.
// System messages are handled separately; this returns either a string or
// []map parts (text + image source blocks).
func anthropicMessageContent(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []Part:
		if len(v) == 0 {
			return ""
		}
		if allTextParts(v) {
			return joinPartTexts(v)
		}
		out := make([]map[string]any, 0, len(v))
		for _, p := range v {
			switch {
			case p.Type == "image" || p.Source != nil:
				src := p.Source
				if src == nil {
					continue
				}
				out = append(out, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       firstNonEmpty(src.Type, "base64"),
						"media_type": src.MediaType,
						"data":       src.Data,
					},
				})
			case p.Type == "image_url" && p.ImageURL != nil:
				media, data, ok := parseDataURL(p.ImageURL.URL)
				if !ok {
					// Non-data URLs are not supported on Anthropic; drop with alt text.
					if p.Text != "" {
						out = append(out, map[string]any{"type": "text", "text": p.Text})
					}
					continue
				}
				out = append(out, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": media,
						"data":       data,
					},
				})
			default:
				text := p.Text
				if text == "" {
					continue
				}
				out = append(out, map[string]any{"type": "text", "text": text})
			}
		}
		if len(out) == 0 {
			return ""
		}
		return out
	default:
		return fmt.Sprint(v)
	}
}

// openAIResponsesContent maps Message.Content for the OpenAI Responses API
// input items. Multimodal content uses input_text / input_image part types.
func openAIResponsesContent(content any) any {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []Part:
		if len(v) == 0 {
			return ""
		}
		if allTextParts(v) {
			return joinPartTexts(v)
		}
		out := make([]map[string]any, 0, len(v))
		for _, p := range v {
			switch {
			case p.Type == "image_url" || p.Type == "input_image" || p.ImageURL != nil:
				url := ""
				if p.ImageURL != nil {
					url = p.ImageURL.URL
				}
				out = append(out, map[string]any{
					"type":      "input_image",
					"image_url": url,
				})
			case p.Type == "image" && p.Source != nil:
				url := fmt.Sprintf("data:%s;base64,%s", p.Source.MediaType, p.Source.Data)
				out = append(out, map[string]any{
					"type":      "input_image",
					"image_url": url,
				})
			default:
				text := p.Text
				if text == "" {
					continue
				}
				out = append(out, map[string]any{"type": "input_text", "text": text})
			}
		}
		if len(out) == 0 {
			return ""
		}
		return out
	default:
		return fmt.Sprint(v)
	}
}

func allTextParts(parts []Part) bool {
	for _, p := range parts {
		if p.Type == "image_url" || p.Type == "image" || p.Type == "input_image" || p.ImageURL != nil || p.Source != nil {
			return false
		}
	}
	return true
}

func joinPartTexts(parts []Part) string {
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p.Text) != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// parseDataURL extracts media type and raw base64 payload from a data: URL.
func parseDataURL(raw string) (media, data string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(raw, "data:")
	media, after, found := strings.Cut(rest, ";")
	if !found {
		return "", "", false
	}
	const b64mark = "base64,"
	if !strings.HasPrefix(after, b64mark) {
		// allow "base64," only
		idx := strings.Index(after, b64mark)
		if idx < 0 {
			return "", "", false
		}
		after = after[idx:]
	}
	data = strings.TrimPrefix(after, b64mark)
	if media == "" || data == "" {
		return "", "", false
	}
	return media, data, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// DataURL builds a data: URL for an image MIME + base64 payload.
func DataURL(mime, dataB64 string) string {
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "application/octet-stream"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, dataB64)
}
