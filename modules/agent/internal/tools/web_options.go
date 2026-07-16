package tools

import (
	"os"
	"regexp"
	"strings"
)

// Per-run web tool option resolution (max results, bytes, proxy, provider).
// HTML regexes used by search/fetch live here as package-level shared state.
func effectiveWebResultCount(runCtx ToolRunContext, requested int) int {
	if requested > 0 {
		return requested
	}
	if runCtx.Reply != nil && runCtx.Reply.Options.WebSearchMaxResults > 0 {
		return runCtx.Reply.Options.WebSearchMaxResults
	}
	return defaultWebResults
}

// effectiveWebFetchBytes 对抓取响应体上限应用相同的优先级规则。
func effectiveWebFetchBytes(runCtx ToolRunContext, requested int) int {
	if requested > 0 {
		return requested
	}
	if runCtx.Reply != nil && runCtx.Reply.Options.WebFetchMaxBytes > 0 {
		return runCtx.Reply.Options.WebFetchMaxBytes
	}
	return maxWebFetchBytes
}

// effectiveWebHTTPProxy 优先使用单次运行选项，其次使用 RED_PANDA_WEB_HTTP_PROXY。
func effectiveWebHTTPProxy(runCtx ToolRunContext) string {
	if runCtx.Reply != nil {
		if proxy := strings.TrimSpace(runCtx.Reply.Options.WebHTTPProxy); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(os.Getenv("RED_PANDA_WEB_HTTP_PROXY"))
}

func effectiveWebSearchOptions(runCtx ToolRunContext) webSearchOptions {
	opts := webSearchOptions{
		Provider: "auto",
		Proxy:    effectiveWebHTTPProxy(runCtx),
	}
	if runCtx.Reply != nil {
		if provider := strings.TrimSpace(runCtx.Reply.Options.WebSearchProvider); provider != "" {
			opts.Provider = provider
		}
		if key := strings.TrimSpace(runCtx.Reply.Options.WebTavilyAPIKey); key != "" {
			opts.TavilyAPIKey = key
		}
	}
	if envProvider := strings.TrimSpace(os.Getenv("RED_PANDA_WEB_SEARCH_PROVIDER")); envProvider != "" && opts.Provider == "auto" {
		// 仅当运行未设置非默认值时使用环境变量中的提供方。
		// 空的回复选项默认值已是 auto。
		if runCtx.Reply == nil || strings.TrimSpace(runCtx.Reply.Options.WebSearchProvider) == "" {
			opts.Provider = envProvider
		}
	}
	return opts
}

// DDG HTML 结构中，class 为 result__a 的锚点包含标题和 href，
// 后续 class 为 result__snippet 的锚点包含摘要。href 可能是
// //duckduckgo.com/l/?uddg=<encoded>&rut=... 形式的跳转链接，必须解码。
var (
	ddgResultLinkRE = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE    = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	// ddgSponsoredRE 删除整个推广结果块：广告结果 div 的结构平坦，
	// 因此从其开始标签匹配至 result__a 链接之后最近的 </div>。
	ddgSponsoredRE   = regexp.MustCompile(`(?s)<div[^>]*class="[^"]*result--ad[^"]*".*?</div>`)
	htmlTagRE        = regexp.MustCompile(`<[^>]*>`)
	htmlWhitespaceRE = regexp.MustCompile(`[ \t\r\f\v]+`)
	htmlNewlineRE    = regexp.MustCompile(`\n{3,}`)
	scriptBlockRE    = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	htmlCommentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTitleRE      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)
