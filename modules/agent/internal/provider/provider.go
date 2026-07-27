package provider

import (
	"context"

	"redpanda/protocol/tools"
)

type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request, emit func(ProviderChunk) error) error
}

// Message is one chat turn sent to a provider. Content is either a plain
// string (text-only, backward compatible) or []Part for multimodal turns
// (docs/51 §6.6 / docs/52 Slice C). Adapters serialize Content appropriately;
// pure-text paths keep Content as string so existing golden JSON stays valid.
type Message struct {
	Role    string
	Content any // string | []Part
}

// Part is one multimodal content part (OpenAI-compatible / Anthropic / Responses).
// Exactly one of Text / ImageURL / Source is typically set depending on Type.
type Part struct {
	Type     string      `json:"type"` // text | image_url | image | input_text | input_image
	Text     string      `json:"text,omitempty"`
	ImageURL *ImageURL   `json:"image_url,omitempty"` // OpenAI chat / Responses data URL
	Source   *ImageSource `json:"source,omitempty"`    // Anthropic base64 source
}

// ImageURL is the OpenAI-compatible image payload (data: URL or https).
type ImageURL struct {
	URL string `json:"url"`
}

// ImageSource is the Anthropic image source block.
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g. image/png
	Data      string `json:"data"`       // raw base64, no data: prefix
}

// RequestAttachment is an ephemeral image payload carried from Gateway→Runtime
// (docs/51 §5.3). Never persisted; used by PromptComposer / Echo to assemble
// provider Parts for the current user turn (and optionally rehydrated history).
type RequestAttachment struct {
	ID       string `json:"id,omitempty"`
	Path     string `json:"path,omitempty"`
	MIME     string `json:"mime,omitempty"`
	DataB64  string `json:"data_b64,omitempty"`
	ByteSize int64  `json:"byte_size,omitempty"`
	Alt      string `json:"alt,omitempty"`
}

type RequestOptions struct {
	ProviderName    string
	ProviderBaseURL string
	ProviderAPIKey  string
	// ProviderHTTPProxy is an optional HTTP(S)/SOCKS5 proxy applied to this
	// provider's HTTP client. Empty falls back to environment proxy.
	ProviderHTTPProxy string
	Stream            *bool
	Model             string
	ReasoningEffort   string
	LogLLMRequests    bool
}

type Request struct {
	RunID     string
	SessionID string
	Input     string
	Messages  []Message
	Options   RequestOptions
	Tools     []tools.Definition
	// ToolHistory 是平铺列表，供 EchoProvider 和测试使用。
	ToolHistory []ToolExchange
	// ToolRounds 对同一模型回合运行的工具分组，符合 OpenAI 多工具格式。
	// 为空时，每个 ToolHistory 项视为独立回合。
	ToolRounds [][]ToolExchange
	// Attachments carries ephemeral multimodal payloads for this request
	// (docs/51 strategy A). Echo reads them for a receipt; adapters consume
	// the Parts already assembled into Messages by PromptComposer.
	Attachments []RequestAttachment
}

// ProviderRequest is kept as a source-compatible alias for provider fakes.
// New production code should use Request.
type ProviderRequest = Request

type ToolExchange struct {
	Call   tools.Call
	Result tools.Result
}

type ProviderChunk struct {
	Delta     string
	Final     bool
	ToolCalls []tools.Call
}

// MessageText returns the plain-text view of a Message.Content value. Multimodal
// parts contribute their Text fields only (image parts are omitted). Used by
// tests and diagnostics; adapters must not use this for vision-capable sends.
func MessageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []Part:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		out := parts[0]
		for i := 1; i < len(parts); i++ {
			out += "\n" + parts[i]
		}
		return out
	default:
		return ""
	}
}
