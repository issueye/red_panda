package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/methods"
)

func TestParseDuckDuckGoHTML(t *testing.T) {
	body := `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdoc">Example</a><a class="result__snippet">Useful <b>snippet</b>.</a></div>`
	items := parseDuckDuckGoHTML(body, 5)
	if len(items) != 1 || items[0].URL != "https://example.com/doc" || items[0].Title != "Example" || !strings.Contains(items[0].Snippet, "Useful snippet") {
		t.Fatalf("items = %#v", items)
	}
}

func TestHandlerWebSearchViaDuckDuckGo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<a class="result__a" href="https://example.com">Example</a><a class="result__snippet">Result text</a>`)
	}))
	defer server.Close()
	previous := duckDuckGoHTMLEndpoint
	duckDuckGoHTMLEndpoint = server.URL
	t.Cleanup(func() { duckDuckGoHTMLEndpoint = previous })

	result, err := HandlerWebSearch(context.Background(), &registry.ToolContext{Reply: &methods.ReplyParams{Options: methods.ReplyOptions{WebSearchProvider: "duckduckgo"}}}, map[string]any{"query": "red panda"})
	if err != nil || result.Status != "completed" || !strings.Contains(result.Output, "https://example.com") {
		t.Fatalf("search result = %#v, %v", result, err)
	}
}

func TestHandlerWebSearchRejectsMissingInputAndTavilyKey(t *testing.T) {
	if _, err := HandlerWebSearch(context.Background(), &registry.ToolContext{}, nil); err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("missing query error = %v", err)
	}
	t.Setenv("RED_PANDA_TAVILY_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	_, err := HandlerWebSearch(context.Background(), &registry.ToolContext{Reply: &methods.ReplyParams{Options: methods.ReplyOptions{WebSearchProvider: "tavily"}}}, map[string]any{"query": "x"})
	if err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("missing Tavily key error = %v", err)
	}
}

func TestHandlerWebFetchExtractsDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><head><title>Example</title></head><body><main>Hello red panda.</main></body></html>`)
	}))
	defer server.Close()
	result, err := HandlerWebFetch(context.Background(), &registry.ToolContext{}, map[string]any{"url": server.URL})
	if err != nil || result.Status != "completed" {
		t.Fatalf("fetch result = %#v, %v", result, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Output), &payload); err != nil || payload["title"] != "Example" || !strings.Contains(fmt.Sprint(payload["text"]), "Hello red panda") {
		t.Fatalf("fetch payload = %#v, %v", payload, err)
	}
}

func TestParseWebProxyURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:7890", "socks5://127.0.0.1:1080", "127.0.0.1:7890"} {
		if _, err := parseWebProxyURL(raw); err != nil {
			t.Errorf("parseWebProxyURL(%q): %v", raw, err)
		}
	}
	if err := applyWebProxy(&http.Transport{}, "ftp://example.com"); err == nil {
		t.Fatal("expected unsupported proxy scheme error")
	}
}
