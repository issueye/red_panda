package tools

import (
	"time"
)

// Shared web tool constants, endpoints, and option types (docs/41 W5-2).

const (
	maxWebFetchBytes  = 2 * 1024 * 1024
	defaultWebResults = 8
	// webRequestTimeout 是单次搜索或抓取尝试的硬性时限。
	// 严格限制时限，避免 UI 长时间停留在“进行中”。
	webRequestTimeout      = 20 * time.Second
	webDialTimeout         = 8 * time.Second
	webTLSHandshakeTimeout = 10 * time.Second
	webResponseHeaderTO    = 15 * time.Second
	// webToolHardTimeout 覆盖完整工具调用（代理检查、请求和响应体读取）。
	webToolHardTimeout     = 25 * time.Second
	webUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	defaultDDGHTMLEndpoint = "https://html.duckduckgo.com/html/"
	defaultDDGJSONEndpoint = "https://api.duckduckgo.com/"
	defaultTavilyEndpoint  = "https://api.tavily.com/search"
)

// duckDuckGoHTMLEndpoint 和 tavilySearchEndpoint 为包级变量，
// 测试可将它们重定向到本地 httptest 服务器。
var duckDuckGoHTMLEndpoint = defaultDDGHTMLEndpoint
var tavilySearchEndpoint = defaultTavilyEndpoint

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
