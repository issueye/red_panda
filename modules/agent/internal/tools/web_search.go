package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// runWebSearch 返回结构化的搜索结果。
//
// 服务提供方优先级（检查项 O6，此处为唯一事实来源）：
//   - auto：配置 API 密钥时使用 Tavily，否则使用 DuckDuckGo
//   - tavily：仅使用 Tavily，未配置密钥时返回错误
//   - duckduckgo：使用 DDG HTML/JSON 回退路径，生产环境优先使用 Tavily
func runWebSearch(ctx context.Context, query string, maxResults int, opts webSearchOptions) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = defaultWebResults
	}
	proxy := strings.TrimSpace(opts.Proxy)
	if err := ensureWebProxyReachable(ctx, proxy); err != nil {
		return "", annotateWebError(err, proxy)
	}
	client, err := newWebHTTPClient(proxy)
	if err != nil {
		return "", annotateWebError(err, proxy)
	}

	provider := normalizeWebSearchProvider(opts.Provider)
	tavilyKey := strings.TrimSpace(opts.TavilyAPIKey)
	if tavilyKey == "" {
		tavilyKey = strings.TrimSpace(os.Getenv("RED_PANDA_TAVILY_API_KEY"))
	}
	if tavilyKey == "" {
		tavilyKey = strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	}

	var (
		items   []webSearchItem
		source  string
		answer  string
		lastErr error
	)

	tryTavily := provider == "tavily" || (provider == "auto" && tavilyKey != "")
	tryDDG := provider == "duckduckgo" || provider == "auto"

	if tryTavily {
		if tavilyKey == "" {
			if provider == "tavily" {
				return "", fmt.Errorf("tavily requires an API key (settings web_tavily_api_key or RED_PANDA_TAVILY_API_KEY)")
			}
		} else {
			tavilyItems, tavilyAnswer, tavilyErr := searchTavily(ctx, client, tavilyKey, query, maxResults)
			if tavilyErr == nil && (len(tavilyItems) > 0 || strings.TrimSpace(tavilyAnswer) != "") {
				items = tavilyItems
				answer = tavilyAnswer
				source = "tavily"
			} else {
				lastErr = tavilyErr
				if provider == "tavily" {
					if tavilyErr != nil {
						return "", annotateWebError(fmt.Errorf("tavily search failed: %w", tavilyErr), proxy)
					}
					return "", annotateWebError(fmt.Errorf("tavily returned no results"), proxy)
				}
			}
		}
	}

	if source == "" && tryDDG {
		ddgItems, ddgSource, ddgErr := searchDuckDuckGo(ctx, client, query, maxResults)
		if ddgErr == nil && len(ddgItems) > 0 {
			items = ddgItems
			source = ddgSource
		} else {
			if lastErr != nil && ddgErr != nil {
				return "", annotateWebError(fmt.Errorf("web.search failed: tavily=%v; duckduckgo=%v", lastErr, ddgErr), proxy)
			}
			if ddgErr != nil {
				return "", annotateWebError(ddgErr, proxy)
			}
			if lastErr != nil {
				return "", annotateWebError(fmt.Errorf("web.search failed: tavily=%v; duckduckgo returned no results", lastErr), proxy)
			}
			return "", annotateWebError(fmt.Errorf("web.search returned no results"), proxy)
		}
	}

	if source == "" {
		if lastErr != nil {
			return "", annotateWebError(lastErr, proxy)
		}
		return "", annotateWebError(fmt.Errorf("web.search returned no results"), proxy)
	}

	payload := map[string]any{
		"action":   "web.search",
		"query":    query,
		"count":    len(items),
		"source":   source,
		"provider": provider,
		"proxy":    proxyLabel(proxy),
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

func normalizeWebSearchProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tavily":
		return "tavily"
	case "duckduckgo", "ddg":
		return "duckduckgo"
	case "auto", "":
		return "auto"
	default:
		return "auto"
	}
}

func searchDuckDuckGo(ctx context.Context, client *http.Client, query string, maxResults int) ([]webSearchItem, string, error) {
	items, htmlErr := searchDuckDuckGoHTML(ctx, client, query, maxResults)
	if htmlErr == nil && len(items) > 0 {
		return items, "duckduckgo_html", nil
	}
	jsonItems, jsonErr := searchDuckDuckGoJSON(ctx, client, query, maxResults)
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

func searchTavily(ctx context.Context, client *http.Client, apiKey string, query string, maxResults int) ([]webSearchItem, string, error) {
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
	if maxResults <= 0 {
		maxResults = defaultWebResults
	}
	items := make([]webSearchItem, 0, len(parsed.Results))
	for _, result := range parsed.Results {
		if len(items) >= maxResults {
			break
		}
		title := strings.TrimSpace(result.Title)
		rawURL := strings.TrimSpace(result.URL)
		snippet := strings.TrimSpace(result.Content)
		if title == "" && snippet == "" {
			continue
		}
		if title == "" {
			title = snippet
		}
		items = append(items, webSearchItem{
			Title:   title,
			URL:     rawURL,
			Snippet: snippet,
			Score:   result.Score,
		})
	}
	return items, strings.TrimSpace(parsed.Answer), nil
}

func searchDuckDuckGoHTML(ctx context.Context, client *http.Client, query string, maxResults int) ([]webSearchItem, error) {
	form := url.Values{}
	form.Set("q", query)
	form.Set("b", "1")

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(400 * time.Millisecond):
			}
		}
		reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, duckDuckGoHTMLEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", webUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			if !isRetriableWebError(err) {
				return nil, err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(maxWebFetchBytes)))
		resp.Body.Close()
		cancel()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			lastErr = fmt.Errorf("search returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 256)])))
			if resp.StatusCode >= 500 || resp.StatusCode == 429 {
				continue
			}
			return nil, lastErr
		}
		items := parseDuckDuckGoHTML(string(body), maxResults)
		return items, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("search failed")
	}
	return nil, lastErr
}

func searchDuckDuckGoJSON(ctx context.Context, client *http.Client, query string, maxResults int) ([]webSearchItem, error) {
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
			title = snippet
		}
		items = append(items, webSearchItem{Title: title, URL: rawURL, Snippet: snippet})
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
		appendItem(item.Text, item.FirstURL, item.Text)
	}
	return items, nil
}

func isRetriableWebError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "eof")
}

// runWebFetch 下载单个 HTTP(S) URL 并返回可读文本。
func parseDuckDuckGoHTML(body string, maxResults int) []webSearchItem {
	// 解析自然搜索结果前先移除推广和广告块。
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
		resolvedURL := resolveDuckDuckGoURL(href)
		if resolvedURL == "" {
			continue
		}
		item := webSearchItem{Title: title, URL: resolvedURL}
		if i < len(snippetMatches) {
			item.Snippet = cleanHTMLText(snippetMatches[i][1])
		}
		items = append(items, item)
	}
	return items
}

func resolveDuckDuckGoURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	// 协议相对的跳转链接：//duckduckgo.com/l/?uddg=<encoded>。
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
	// 已是直接的 HTTP(S) 链接。
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return href
	}
	return ""
}

func extractTextFromHTML(body string) string {
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(cleanHTMLText(m[1]))
	}

	// 完整移除 head/title 块，避免其文本在正文提取中重复，
	// 随后移除注释、脚本和样式块。
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
	value = strings.ReplaceAll(value, "&nbsp;", " ")
	value = htmlNewlineRE.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
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
