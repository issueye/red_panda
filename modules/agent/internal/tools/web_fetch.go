package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// web.fetch implementation and HTML/text extraction.
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

// extractTitleAndText 将抓取的字节内容转换为供代理使用的可读纯文本。
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

// extractHTMLDocument 返回页面标题和清理后的正文，并尽量减少导航等页面框架内容。
func extractHTMLDocument(body string) (string, string) {
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(body); len(m) > 1 {
		title = strings.TrimSpace(cleanHTMLText(m[1]))
	}
	text := extractTextFromHTML(body)
	// 移除对模型无用的高噪声残留内容。
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
		// 移除纯页面框架片段。
		if strings.HasPrefix(trim, "<") && strings.HasSuffix(trim, ">") {
			continue
		}
		kept = append(kept, trim)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
