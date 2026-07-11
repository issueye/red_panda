package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	maxWebFetchBytes       = 2 * 1024 * 1024
	defaultWebResults      = 8
	webRequestTimeout      = 20 * time.Second
	webUserAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	defaultDDGHTMLEndpoint = "https://html.duckduckgo.com/html/"
)

// duckDuckGoHTMLEndpoint is a package variable so tests can redirect it at a
// local httptest server instead of hitting the real network.
var duckDuckGoHTMLEndpoint = defaultDDGHTMLEndpoint

type webSearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// runWebSearch queries the DuckDuckGo HTML endpoint and returns structured results.
// It does not require an API key and never follows sponsored results.
func runWebSearch(ctx context.Context, query string, maxResults int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = defaultWebResults
	}

	form := url.Values{}
	form.Set("q", query)
	form.Set("b", "1") // boilerplate-free output

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, duckDuckGoHTMLEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := newWebHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("search returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxWebFetchBytes)))
	if err != nil {
		return "", err
	}
	items := parseDuckDuckGoHTML(string(body), maxResults)

	payload := map[string]any{
		"action": "web.search",
		"query":  query,
		"count":  len(items),
		"items":  items,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return truncateToolOutput(string(raw)), nil
}

// runWebFetch downloads a single http(s) URL and returns readable text.
func runWebFetch(ctx context.Context, rawURL string, maxBytes int) (string, error) {
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

	reqCtx, cancel := context.WithTimeout(ctx, webRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", webUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.5")

	resp, err := newWebHTTPClient().Do(req)
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
	return truncateToolOutput(extractTextFromHTML(string(body))), nil
}

func newWebHTTPClient() *http.Client {
	return &http.Client{Timeout: webRequestTimeout}
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
