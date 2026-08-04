package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"redpanda/agent/internal/runtime/registry"
	ptools "redpanda/protocol/tools"
)

// ---------------------------------------------------------------------------
// Handler entry points (called from register.go)
// ---------------------------------------------------------------------------

// HandlerWebSearch is the registry handler for web.search.
func HandlerWebSearch(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	raw, err := runWebSearch(ctx, tc, args)
	return WrapResult("web.search", raw, err)
}

// HandlerWebFetch is the registry handler for web.fetch.
func HandlerWebFetch(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	raw, err := runWebFetch(ctx, tc, args)
	return WrapResult("web.fetch", raw, err)
}

// ---------------------------------------------------------------------------
// web.search implementation
// ---------------------------------------------------------------------------

func runWebSearch(ctx context.Context, tc *registry.ToolContext, args map[string]any) (string, error) {
	query := StrArg(args, "query")
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	maxResults := IntArg(args, "max_results", defaultWebResults)
	opts := effectiveWebSearchOptionsForPlugin(tc)

	var (
		items   []webSearchItem
		source  string
		answer  string
		lastErr error
	)

	tryTavily := opts.Provider == "tavily" || (opts.Provider == "auto" && opts.TavilyAPIKey != "")
	tryDDG := opts.Provider == "duckduckgo" || opts.Provider == "auto"

	if tryTavily {
		if opts.TavilyAPIKey == "" && opts.Provider == "tavily" {
			return "", fmt.Errorf("tavily requires an API key")
		}
		if opts.TavilyAPIKey != "" {
			tItems, tAnswer, tErr := searchTavily(ctx, opts.Proxy, opts.TavilyAPIKey, query, maxResults)
			if tErr == nil && (len(tItems) > 0 || strings.TrimSpace(tAnswer) != "") {
				items, answer, source = tItems, tAnswer, "tavily"
			} else {
				lastErr = tErr
				if opts.Provider == "tavily" {
					if tErr != nil {
						return "", fmt.Errorf("tavily search failed: %w", tErr)
					}
					return "", fmt.Errorf("tavily returned no results")
				}
			}
		}
	}

	if source == "" && tryDDG {
		ddgItems, ddgSource, ddgErr := searchDuckDuckGo(ctx, opts.Proxy, query, maxResults)
		if ddgErr == nil && len(ddgItems) > 0 {
			items, source = ddgItems, ddgSource
		} else {
			if lastErr != nil && ddgErr != nil {
				return "", fmt.Errorf("web.search failed: tavily=%v; duckduckgo=%v", lastErr, ddgErr)
			}
			if ddgErr != nil {
				return "", ddgErr
			}
			if lastErr != nil {
				return "", fmt.Errorf("web.search failed: tavily=%v; duckduckgo returned no results", lastErr)
			}
			return "", fmt.Errorf("web.search returned no results")
		}
	}

	if source == "" {
		if lastErr != nil {
			return "", lastErr
		}
		return "", fmt.Errorf("web.search returned no results")
	}

	payload := map[string]any{
		"action":   "web.search",
		"query":    query,
		"count":    len(items),
		"source":   source,
		"provider": opts.Provider,
		"proxy":    proxyLabel(opts.Proxy),
		"items":    items,
	}
	if strings.TrimSpace(answer) != "" {
		payload["answer"] = strings.TrimSpace(answer)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return TruncateToolOutput(string(raw)), nil
}

// searchTavily calls the Tavily search API.
func searchTavily(ctx context.Context, proxy, apiKey string, query string, maxResults int) ([]webSearchItem, string, error) {
	client, err := newWebHTTPClient(proxy)
	if err != nil {
		return nil, "", err
	}

	body := map[string]any{
		"query":          query,
		"max_results":    maxResults,
		"search_depth":   "basic",
		"include_answer": true,
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, tavilySearchEndpoint, strings.NewReader(string(rawBody)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxWebFetchBytes)))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("tavily returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody[:min(len(respBody), 256)])))
	}
	return parseTavilySearchResponse(respBody, maxResults)
}

func parseTavilySearchResponse(body []byte, maxResults int) ([]webSearchItem, string, error) {
	var parsed struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, "", fmt.Errorf("invalid tavily response: %w", err)
	}
	items := make([]webSearchItem, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		if len(items) >= maxResults {
			break
		}
		title := strings.TrimSpace(r.Title)
		rawURL := strings.TrimSpace(r.URL)
		snippet := strings.TrimSpace(r.Content)
		if title == "" && snippet == "" {
			continue
		}
		if title == "" {
			title = snippet
		}
		items = append(items, webSearchItem{Title: title, URL: rawURL, Snippet: snippet, Score: r.Score})
	}
	return items, strings.TrimSpace(parsed.Answer), nil
}

// searchDuckDuckGo tries HTML then JSON endpoint.
func searchDuckDuckGo(ctx context.Context, proxy string, query string, maxResults int) ([]webSearchItem, string, error) {
	items, htmlErr := searchDuckDuckGoHTML(ctx, proxy, query, maxResults)
	if htmlErr == nil && len(items) > 0 {
		return items, "duckduckgo_html", nil
	}
	jsonItems, jsonErr := searchDuckDuckGoJSON(ctx, proxy, query, maxResults)
	if jsonErr == nil && len(jsonItems) > 0 {
		return jsonItems, "duckduckgo_json", nil
	}
	if htmlErr != nil && jsonErr != nil {
		return nil, "", fmt.Errorf("html=%v; json=%v", htmlErr, jsonErr)
	}
	if htmlErr != nil {
		return nil, "", htmlErr
	}
	if jsonErr != nil {
		return nil, "", jsonErr
	}
	return nil, "", fmt.Errorf("no results")
}

func searchDuckDuckGoHTML(ctx context.Context, proxy string, query string, maxResults int) ([]webSearchItem, error) {
	client, err := newWebHTTPClient(proxy)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("q", query)
	form.Set("b", "1")

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, duckDuckGoHTMLEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxWebFetchBytes)))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("search returned HTTP %d", resp.StatusCode)
	}
	return parseDuckDuckGoHTML(string(body), maxResults), nil
}

func parseDuckDuckGoHTML(body string, maxResults int) []webSearchItem {
	body = ddgSponsoredRE.ReplaceAllString(body, "")
	linkMatches := ddgResultLinkRE.FindAllStringSubmatch(body, maxResults)
	snippetMatches := ddgSnippetRE.FindAllStringSubmatch(body, maxResults)

	items := make([]webSearchItem, 0, len(linkMatches))
	for i, match := range linkMatches {
		href := strings.TrimSpace(match[1])
		title := cleanHTMLText(match[2])
		if title == "" || href == "" {
			continue
		}
		rawURL := resolveDuckDuckGoURL(href)
		if rawURL == "" {
			continue
		}
		item := webSearchItem{Title: cleanHTMLText(title), URL: rawURL}
		if i < len(snippetMatches) {
			item.Snippet = cleanHTMLText(snippetMatches[i][1])
		}
		item = cleanDuckDuckGoItem(item)
		items = append(items, item)
	}
	return items
}

func resolveDuckDuckGoURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.Contains(parsed.Host, "duckduckgo.com") {
		if uddg := parsed.Query().Get("uddg"); uddg != "" {
			if decoded, err := url.QueryUnescape(uddg); err == nil && decoded != "" {
				return decoded
			}
		}
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return href
	}
	return ""
}

func searchDuckDuckGoJSON(ctx context.Context, proxy string, query string, maxResults int) ([]webSearchItem, error) {
	client, err := newWebHTTPClient(proxy)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(defaultDDGJSONEndpoint, "/") + "/"
	values := url.Values{}
	values.Set("q", query)
	values.Set("format", "json")
	values.Set("no_html", "1")
	values.Set("skip_disambig", "1")

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("json search returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxWebFetchBytes)))
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGoJSON(body, maxResults)
}

func parseDuckDuckGoJSON(body []byte, maxResults int) ([]webSearchItem, error) {
	var parsed struct {
		AbstractText   string `json:"AbstractText"`
		AbstractURL    string `json:"AbstractURL"`
		AbstractSource string `json:"AbstractSource"`
		Heading        string `json:"Heading"`
		Answer         string `json:"Answer"`
		AnswerType     string `json:"AnswerType"`
		RelatedTopics  []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
		Results []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"Results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	items := make([]webSearchItem, 0, maxResults)
	appendItem := func(title, rawURL, snippet string) {
		if len(items) >= maxResults {
			return
		}
		title = strings.TrimSpace(title)
		rawURL = strings.TrimSpace(rawURL)
		snippet = strings.TrimSpace(snippet)
		if title == "" && snippet == "" {
			return
		}
		if title == "" {
			// Use snippet as title.
			title = snippet
		}
		items = append(items, cleanDuckDuckGoItem(webSearchItem{Title: title, URL: rawURL, Snippet: snippet}))
	}

	if parsed.AbstractText != "" || parsed.AbstractURL != "" {
		title := parsed.Heading
		if title == "" {
			title = parsed.AbstractSource
		}
		if title == "" {
			title = "Abstract"
		}
		appendItem(title, parsed.AbstractURL, parsed.AbstractText)
	}
	if parsed.Answer != "" {
		appendItem("Answer", "", parsed.Answer)
	}
	for _, item := range parsed.Results {
		appendItem(item.Text, item.FirstURL, item.Text)
	}
	for _, item := range parsed.RelatedTopics {
		// Only include if it has meaningful content.
		if item.Text != "" {
			appendItem(item.Text, item.FirstURL, item.Text)
		}
	}
	return items, nil
}

// ---------------------------------------------------------------------------
// web.fetch implementation
// ---------------------------------------------------------------------------

func runWebFetch(ctx context.Context, tc *registry.ToolContext, args map[string]any) (string, error) {
	rawURL := StrArg(args, "url")
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("url host is required")
	}
	maxBytes := IntArg(args, "max_bytes", maxWebFetchBytes)

	client, err := newWebHTTPClient(optsProxyFromEnv())
	if err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("fetch returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return "", err
	}
	contentType := resp.Header.Get("Content-Type")
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	title, text := extractTitleAndText(string(body), contentType)
	truncated := len(body) >= maxBytes

	payload := map[string]any{
		"action":       "web.fetch",
		"url":          rawURL,
		"final_url":    finalURL,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"title":        title,
		"text":         text,
		"bytes":        len(body),
		"truncated":    truncated,
		"proxy":        proxyLabel(optsProxyFromEnv()),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ---------------------------------------------------------------------------
// HTML extraction helpers (adapted from web_fetch.go + web_search.go)
// ---------------------------------------------------------------------------

func extractTitleAndText(body string, contentType string) (string, string) {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "application/json") || looksLikeJSON(body) {
		compact := strings.TrimSpace(body)
		if len(compact) > maxWebFetchBytes {
			compact = compact[:maxWebFetchBytes] + "\n…[truncated]"
		}
		return "", compact
	}
	if strings.Contains(ct, "text/plain") || (!strings.Contains(ct, "html") && !strings.Contains(body, "<html") && !strings.Contains(body, "<HTML")) {
		text := strings.TrimSpace(body)
		if len(text) > maxWebFetchBytes {
			text = text[:maxWebFetchBytes] + "\n…[truncated]"
		}
		return "", text
	}
	title, text := extractHTMLDocument(body)
	if len(text) > maxWebFetchBytes {
		text = text[:maxWebFetchBytes] + "\n…[truncated]"
	}
	return title, text
}

func looksLikeJSON(body string) bool {
	trim := strings.TrimSpace(body)
	return strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[")
}

func extractHTMLDocument(body string) (string, string) {
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(cleanHTMLText(m[1]))
	}
	text := extractTextFromHTML(body)
	text = collapseBoilerplateNoise(text)
	return title, text
}

func extractTextFromHTML(body string) string {
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(cleanHTMLText(m[1]))
	}
	cleaned := htmlTitleRE.ReplaceAllString(body, "")
	cleaned = htmlCommentRE.ReplaceAllString(cleaned, "")
	cleaned = scriptBlockRE.ReplaceAllString(cleaned, "")
	cleaned = htmlTagRE.ReplaceAllString(cleaned, " ")
	cleaned = cleanHTMLText(cleaned)
	if title != "" {
		return title + "\n\n" + cleaned
	}
	return cleaned
}

func cleanHTMLText(value string) string {
	value = htmlTagRE.ReplaceAllString(value, " ")
	value = decodeCommonEntities(value)
	value = htmlWhitespaceRE.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = htmlNewlineRE.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}

// cleanDuckDuckGoItem 对单个 DDG 搜索结果条目做数据清洗：
// 折叠连续空白、去除尾部省略号/分隔符、清理标题与摘要中的冗余标点，
// 并去除与标题重复的摘要，避免给模型返回脏数据。
func cleanDuckDuckGoItem(item webSearchItem) webSearchItem {
	item.Title = strings.TrimSpace(collapseDuckDuckGoWhitespace(item.Title))
	item.Snippet = strings.TrimSpace(collapseDuckDuckGoWhitespace(item.Snippet))
	item.Title = trimTrailingEllipsis(item.Title)
	item.Snippet = trimTrailingEllipsis(item.Snippet)
	// 若摘要与标题完全重复，则清空摘要，减少冗余信息。
	if item.Snippet != "" && item.Snippet == item.Title {
		item.Snippet = ""
	}
	return item
}

// trimTrailingEllipsis 去除字符串末尾的省略号（中英文省略号或连续 3 个及以上点号），
// 保留业务相关的单个句号。
func trimTrailingEllipsis(value string) string {
	value = strings.TrimRight(value, " \t")
	// 统计末尾连续点号数量。
	dotCount := 0
	for i := len(value) - 1; i >= 0 && value[i] == '.'; i-- {
		dotCount++
	}
	// 中英文省略号单字符（按 rune 计数）。
	runes := []rune(value)
	ellipsisLen := 0
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '…' || runes[i] == '⋯' {
			ellipsisLen++
		} else {
			break
		}
	}
	if ellipsisLen > 0 {
		value = strings.TrimRight(string(runes[:len(runes)-ellipsisLen]), " \t")
	} else if dotCount >= 3 {
		value = strings.TrimRight(value[:len(value)-dotCount], " \t")
	}
	return value
}

// collapseDuckDuckGoWhitespace 将连续空白（含换行、制表符、CR）折叠为单个空格。
func collapseDuckDuckGoWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return htmlWhitespaceRE.ReplaceAllString(value, " ")
}

func decodeCommonEntities(value string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
	)
	return replacer.Replace(value)
}

func collapseBoilerplateNoise(text string) string {
	lines := strings.Split(text, "\n")
	noise := []string{
		"skip to content", "toggle navigation", "sign in", "sign up",
		"appearance settings", "you signed in with another tab",
		"notifications you must be signed in",
	}
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if len(kept) > 0 && kept[len(kept)-1] != "" {
				kept = append(kept, "")
			}
			continue
		}
		lower := strings.ToLower(trim)
		skip := false
		for _, n := range noise {
			if lower == n || strings.HasPrefix(lower, n) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if strings.HasPrefix(trim, "<") && strings.HasSuffix(trim, ">") {
			continue
		}
		kept = append(kept, trim)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// ---------------------------------------------------------------------------
// Web HTTP client + proxy
// ---------------------------------------------------------------------------

func newWebHTTPClient(proxy string) (*http.Client, error) {
	dialer := &net.Dialer{
		Timeout:   webDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   webTLSHandshakeTimeout,
		ResponseHeaderTimeout: webResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
	}
	if err := applyWebProxy(transport, proxy); err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   webRequestTimeout,
		Transport: transport,
	}, nil
}

func applyWebProxy(transport *http.Transport, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := parseWebProxyURL(raw)
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
		return nil
	case "socks5", "socks5h":
		transport.Proxy = nil
		return fmt.Errorf("SOCKS5 proxy not supported in v0.3.0 (use http/https)")
	default:
		return fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}

func parseWebProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	return parsed, nil
}

func proxyLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" {
			return "env"
		}
		return "none"
	}
	parsed, err := parseWebProxyURL(raw)
	if err != nil {
		return "invalid"
	}
	// Strip credentials.
	scheme := parsed.Scheme
	host := parsed.Host
	if u := parsed.User; u != nil {
		if _, ok := parsed.User.Password(); ok {
			host = u.Username() + "@\" + host"
		}
	}
	return scheme + "://" + host
}

func optsProxyFromEnv() string {
	return strings.TrimSpace(os.Getenv("RED_PANDA_WEB_HTTP_PROXY"))
}

// ---------------------------------------------------------------------------
// Options helpers
// ---------------------------------------------------------------------------

func effectiveWebSearchOptionsForPlugin(tc *registry.ToolContext) webSearchOptions {
	opts := webSearchOptions{Provider: "auto"}
	opts.Proxy = optsProxyFromEnv()
	// Check reply options if available.
	if tc != nil && tc.Reply != nil {
		if p := strings.TrimSpace(tc.Reply.Options.WebSearchProvider); p != "" {
			opts.Provider = p
		}
		if k := strings.TrimSpace(tc.Reply.Options.WebTavilyAPIKey); k != "" {
			opts.TavilyAPIKey = k
		}
	}
	// Fall back to env.
	if opts.Provider == "auto" {
		if ep := strings.TrimSpace(os.Getenv("RED_PANDA_WEB_SEARCH_PROVIDER")); ep != "" {
			opts.Provider = ep
		}
	}
	if opts.TavilyAPIKey == "" {
		opts.TavilyAPIKey = strings.TrimSpace(os.Getenv("RED_PANDA_TAVILY_API_KEY"))
	}
	if opts.TavilyAPIKey == "" {
		opts.TavilyAPIKey = strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	}
	return opts
}

// ---------------------------------------------------------------------------
// DDG HTML regexes (from web_options.go)
// ---------------------------------------------------------------------------

var (
	ddgResultLinkRE  = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE     = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	ddgSponsoredRE   = regexp.MustCompile(`(?s)<div[^>]*class="[^"]*result--ad[^"]*".*?</div>`)
	htmlTagRE        = regexp.MustCompile(`<[^>]*>`)
	htmlWhitespaceRE = regexp.MustCompile(`[ \t\r\f\v]+`)
	htmlNewlineRE    = regexp.MustCompile(`\n{3,}`)
	scriptBlockRE    = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	htmlCommentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTitleRE      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)
