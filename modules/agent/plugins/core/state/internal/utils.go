package internal

import (
	"strings"
	"time"

	ptools "redpanda/protocol/tools"
)

// Helper functions copied from modules/agent/internal/tools/helpers.go
// and shared web constants/types used by web tools and the registry
// registration layer.

const maxToolOutputBytes = 64 * 1024

func WrapResult(name string, output string, err error) (*ptools.Result, error) {
	if err != nil {
		return &ptools.Result{
			Name:   name,
			Status: ptools.CallStatusFailed,
			Output: output,
			Error:  err.Error(),
		}, err
	}
	return &ptools.Result{
		Name:   name,
		Status: ptools.CallStatusCompleted,
		Output: output,
	}, nil
}

func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func StrArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func StrArgDefault(args map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(StrArg(args, key))
	if value == "" {
		return fallback
	}
	return value
}

func IntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func boolArgPresent(args map[string]any, key string) (bool, bool) {
	value, ok := args[key].(bool)
	return value, ok
}

func TruncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
}

type jsonNumber interface {
	Int64() (int64, error)
}

// ---------------------------------------------------------------------------
// Web tool shared constants, endpoints and option types
// (identical to modules/agent/internal/tools/web_types.go).
// ---------------------------------------------------------------------------

const (
	maxWebFetchBytes          = 2 * 1024 * 1024
	maxWebSearchResults       = 12
	defaultWebResults         = 8
	webRequestTimeout         = 20 * time.Second
	webDialTimeout            = 8 * time.Second
	webTLSHandshakeTimeout    = 10 * time.Second
	webResponseHeaderTimeout  = 15 * time.Second
	webToolHardTimeout        = 25 * time.Second
	webUserAgent              = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	defaultDDGHTMLEndpoint    = "https://html.duckduckgo.com/html/"
	defaultDDGJSONEndpoint    = "https://api.duckduckgo.com/"
	defaultTavilyEndpoint     = "https://api.tavily.com/search"
)

var duckDuckGoHTMLEndpoint = defaultDDGHTMLEndpoint
var tavilySearchEndpoint   = defaultTavilyEndpoint

type webSearchItem struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type webSearchOptions struct {
	Provider     string
	TavilyAPIKey string
	Proxy        string
}
