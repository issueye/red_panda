package runtime

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

	xproxy "golang.org/x/net/proxy"
)

const (
	maxWebFetchBytes  = 2 * 1024 * 1024
	defaultWebResults = 8
	// webRequestTimeout is the hard upper bound for a single search/fetch attempt.
	// Keep it strict so the UI never sits on "进行中" indefinitely.
	webRequestTimeout      = 20 * time.Second
	webDialTimeout         = 8 * time.Second
	webTLSHandshakeTimeout = 10 * time.Second
	webResponseHeaderTO    = 15 * time.Second
	// webToolHardTimeout wraps the full tool call (proxy check + request + body).
	webToolHardTimeout     = 25 * time.Second
	webUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	defaultDDGHTMLEndpoint = "https://html.duckduckgo.com/html/"
	defaultDDGJSONEndpoint = "https://api.duckduckgo.com/"
	defaultTavilyEndpoint  = "https://api.tavily.com/search"
)

// duckDuckGoHTMLEndpoint / tavilySearchEndpoint are package variables so tests
// can redirect them at local httptest servers.
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

// runWebSearch returns structured search results.
//
// Provider priority (checklist O6 — keep this the single source of truth):
//   - auto: Tavily when an API key is configured, otherwise DuckDuckGo
//   - tavily: Tavily only (errors if no key)
//   - duckduckgo: DDG HTML/JSON fallback path (fragile; prefer Tavily in production)
//
// Further DDG HTML parsing may be split to web_tools_ddg.go without changing this contract.
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
	return truncateToolOutput(string(raw)), nil
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

// runWebFetch downloads a single http(s) URL and returns readable text.
func runWebFetch(ctx context.Context, rawURL string, maxBytes int, proxy string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	parsedScheme := strings.ToLower(parsed.Scheme)
	if parsedScheme != "http" && parsedScheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("url host is required")
	}
	if maxBytes <= 0 {
		maxBytes = maxWebFetchBytes
	}

	if err := ensureWebProxyReachable(ctx, proxy); err != nil {
		return "", annotateWebError(err, proxy)
	}
	client, err := newWebHTTPClient(proxy)
	if err != nil {
		return "", annotateWebError(err, proxy)
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
		return "", annotateWebError(err, proxy)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", annotateWebError(fmt.Errorf("fetch returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))), proxy)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return "", annotateWebError(err, proxy)
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
		"proxy":        proxyLabel(proxy),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// extractTitleAndText turns fetched bytes into readable plain text for agents.
func extractTitleAndText(body string, contentType string) (string, string) {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "application/json") || looksLikeJSON(body) {
		compact := strings.TrimSpace(body)
		if len(compact) > maxToolOutputBytes {
			compact = compact[:maxToolOutputBytes] + "\n…[truncated]"
		}
		return "", compact
	}
	if strings.Contains(ct, "text/plain") || (!strings.Contains(ct, "html") && !strings.Contains(body, "<html") && !strings.Contains(body, "<HTML")) {
		text := strings.TrimSpace(body)
		if len(text) > maxToolOutputBytes {
			text = text[:maxToolOutputBytes] + "\n…[truncated]"
		}
		return "", text
	}
	title, text := extractHTMLDocument(body)
	if len(text) > maxToolOutputBytes {
		text = text[:maxToolOutputBytes] + "\n…[truncated]"
	}
	return title, text
}

func looksLikeJSON(body string) bool {
	trim := strings.TrimSpace(body)
	return strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[")
}

// extractHTMLDocument returns page title and cleaned main text (nav/chrome reduced).
func extractHTMLDocument(body string) (string, string) {
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(cleanHTMLText(m[1]))
	}
	text := extractTextFromHTML(body)
	// Drop extremely noisy leftovers that are not useful to models.
	text = collapseBoilerplateNoise(text)
	return title, text
}

func collapseBoilerplateNoise(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	noise := []string{
		"skip to content", "toggle navigation", "sign in", "sign up",
		"appearance settings", "you signed in with another tab",
		"notifications you must be signed in",
	}
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
		// Drop pure chrome fragments.
		if strings.HasPrefix(trim, "<") && strings.HasSuffix(trim, ">") {
			continue
		}
		kept = append(kept, trim)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func newWebHTTPClient(proxy string) (*http.Client, error) {
	dialer := &net.Dialer{
		Timeout:   webDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false, // HTTP/2 via some proxies can hang forever
		TLSHandshakeTimeout:   webTLSHandshakeTimeout,
		ResponseHeaderTimeout: webResponseHeaderTO,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		// Disable keep-alive reuse for one-shot tool calls to avoid sticky half-open conns.
		DisableKeepAlives: true,
	}
	if err := applyWebProxy(transport, proxy); err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   webRequestTimeout,
		Transport: transport,
	}, nil
}

// runWebOp enforces a hard deadline around web tools so a hung dial/proxy/body
// read cannot leave the tool card in "running" forever.
func runWebOp(ctx context.Context, op func(context.Context) (string, error)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithTimeout(ctx, webToolHardTimeout)
	defer cancel()

	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := op(opCtx)
		done <- outcome{out: out, err: err}
	}()

	select {
	case <-opCtx.Done():
		// Prefer the operation error if it races in at the same time.
		select {
		case res := <-done:
			if res.err != nil {
				return "", res.err
			}
			return res.out, nil
		default:
			return "", fmt.Errorf("web request timed out after %s", webToolHardTimeout)
		}
	case res := <-done:
		if res.err != nil {
			// Normalize context deadline into a clear tool error.
			if opCtx.Err() != nil && (res.err == context.DeadlineExceeded || strings.Contains(strings.ToLower(res.err.Error()), "deadline exceeded") || strings.Contains(strings.ToLower(res.err.Error()), "context canceled")) {
				return "", fmt.Errorf("web request timed out after %s: %w", webToolHardTimeout, res.err)
			}
			return "", res.err
		}
		return res.out, nil
	}
}

// applyWebProxy configures transport for an explicit proxy setting.
// Empty proxy keeps ProxyFromEnvironment. Invalid explicit proxy is an error
// (never silently ignored).
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
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{
				User:     parsed.User.Username(),
				Password: password,
			}
		}
		// Base dialer with timeout so SOCKS setup cannot hang forever.
		base := &net.Dialer{Timeout: webDialTimeout, KeepAlive: 30 * time.Second}
		socksDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, base)
		if err != nil {
			return fmt.Errorf("socks5 dialer: %w", err)
		}
		// SOCKS dialer owns connectivity; clear HTTP Proxy to avoid double-proxy.
		transport.Proxy = nil
		if contextDialer, ok := socksDialer.(xproxy.ContextDialer); ok {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				// Bound each dial attempt even if the parent ctx is long-lived.
				dialCtx, cancel := context.WithTimeout(ctx, webDialTimeout+webTLSHandshakeTimeout)
				defer cancel()
				return contextDialer.DialContext(dialCtx, network, address)
			}
		} else {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				type res struct {
					c   net.Conn
					err error
				}
				ch := make(chan res, 1)
				go func() { c, err := socksDialer.Dial(network, address); ch <- res{c, err} }()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(webDialTimeout + webTLSHandshakeTimeout):
					return nil, fmt.Errorf("socks5 dial timed out")
				case r := <-ch:
					return r.c, r.err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported proxy scheme %q (use http://, https://, or socks5://)", parsed.Scheme)
	}
}

func parseWebProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy is empty")
	}
	// Tolerate full-width punctuation that sometimes appears after paste.
	raw = strings.Map(func(r rune) rune {
		switch r {
		case '：':
			return ':'
		case '／':
			return '/'
		default:
			return r
		}
	}, raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		// supported
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (use http://, https://, or socks5://)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("proxy host is required")
	}
	return parsed, nil
}

func proxyLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("ALL_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != "" {
			return "env"
		}
		return "none"
	}
	parsed, err := parseWebProxyURL(raw)
	if err != nil {
		return "invalid"
	}
	// Never leak credentials into tool output / logs.
	return parsed.Scheme + "://" + parsed.Host
}

func annotateWebError(err error, proxy string) error {
	if err == nil {
		return nil
	}
	label := proxyLabel(proxy)
	hint := ""
	msg := strings.ToLower(err.Error())
	switch {
	case label == "none" && (strings.Contains(msg, "tls handshake") || strings.Contains(msg, "timeout")):
		hint = "; 当前未使用代理（proxy=none）。请在设置 → 网络工具填写 HTTP 代理（如 http://127.0.0.1:7890），并确认代理软件已开启"
	case label != "none" && label != "invalid" && (strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused") || strings.Contains(msg, "no connection") || strings.Contains(msg, "无法连接代理")):
		hint = "; 无法连接代理 " + label + "，请确认代理软件已启动且端口正确"
	case label != "none" && label != "invalid" && strings.Contains(msg, "tls handshake"):
		hint = "; 已配置代理 " + label + " 但仍 TLS 超时，请检查代理是否可访问目标站点，或改用 socks5://"
	case label == "invalid":
		hint = "; 代理地址无效"
	}
	return fmt.Errorf("%v (proxy=%s)%s", err, label, hint)
}

// ensureWebProxyReachable fails fast when an explicit proxy host is not listening.
func ensureWebProxyReachable(ctx context.Context, proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil
	}
	parsed, err := parseWebProxyURL(proxy)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return fmt.Errorf("无法连接代理 %s: %w", parsed.Host, err)
	}
	_ = conn.Close()
	return nil
}

// effectiveWebResultCount merges per-run options, explicit tool args, and the
// package default into a single result-count value. Explicit tool args win,
// then the Desktop-transmitted run option, then the default.
func effectiveWebResultCount(runCtx ToolRunContext, requested int) int {
	if requested > 0 {
		return requested
	}
	if runCtx.Reply != nil && runCtx.Reply.Options.WebSearchMaxResults > 0 {
		return runCtx.Reply.Options.WebSearchMaxResults
	}
	return defaultWebResults
}

// effectiveWebFetchBytes applies the same precedence for the fetch body cap.
func effectiveWebFetchBytes(runCtx ToolRunContext, requested int) int {
	if requested > 0 {
		return requested
	}
	if runCtx.Reply != nil && runCtx.Reply.Options.WebFetchMaxBytes > 0 {
		return runCtx.Reply.Options.WebFetchMaxBytes
	}
	return maxWebFetchBytes
}

// effectiveWebHTTPProxy prefers per-run option, then RED_PANDA_WEB_HTTP_PROXY.
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
		// Only use env provider when the run did not set an explicit non-default.
		// Reply options already default to auto when empty.
		if runCtx.Reply == nil || strings.TrimSpace(runCtx.Reply.Options.WebSearchProvider) == "" {
			opts.Provider = envProvider
		}
	}
	return opts
}

// DDG HTML structures: results are anchors with class result__a (title + href)
// and following anchors with class result__snippet. Hrefs may be redirect links
// like //duckduckgo.com/l/?uddg=<encoded>&rut=... which must be decoded.
var (
	ddgResultLinkRE = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE    = regexp.MustCompile(`(?s)<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
	// ddgSponsoredRE drops the whole sponsored result block: the ad result div
	// has a flat structure, so match from its opening tag through the nearest
	// closing </div> that follows its result__a link.
	ddgSponsoredRE   = regexp.MustCompile(`(?s)<div[^>]*class="[^"]*result--ad[^"]*".*?</div>`)
	htmlTagRE        = regexp.MustCompile(`<[^>]*>`)
	htmlWhitespaceRE = regexp.MustCompile(`[ \t\r\f\v]+`)
	htmlNewlineRE    = regexp.MustCompile(`\n{3,}`)
	scriptBlockRE    = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	htmlCommentRE    = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTitleRE      = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func parseDuckDuckGoHTML(body string, maxResults int) []webSearchItem {
	// Drop sponsored/ad blocks before parsing organic results.
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
	// Protocol-relative redirect links: //duckduckgo.com/l/?uddg=<encoded>
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
	// Already a direct http(s) link.
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

	// Remove the head/title block entirely so its text does not duplicate into
	// the body extraction, then strip comments and script/style blocks.
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
