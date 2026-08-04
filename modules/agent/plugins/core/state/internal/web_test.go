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

func TestCleanDuckDuckGoItem(t *testing.T) {
	// 折叠连续空白、去除尾部省略号/分隔符。
	cleaned := cleanDuckDuckGoItem(webSearchItem{
		Title:   "  Red\n   Panda  ... ",
		Snippet: "A   great   article…",
	})
	if cleaned.Title != "Red Panda" {
		t.Errorf("Title = %q, want %q", cleaned.Title, "Red Panda")
	}
	if cleaned.Snippet != "A great article" {
		t.Errorf("Snippet = %q, want %q", cleaned.Snippet, "A great article")
	}

	// 摘要与标题重复时清空摘要。
	dup := cleanDuckDuckGoItem(webSearchItem{Title: "Same", Snippet: "Same"})
	if dup.Snippet != "" {
		t.Errorf("dup Snippet = %q, want empty", dup.Snippet)
	}

	// 保留业务相关句号，仅去除尾部冗余标点；连续空白被折叠为单空格。
	keep := cleanDuckDuckGoItem(webSearchItem{Title: "Go.  Guide.", Snippet: "Learn Go. Now."})
	if keep.Title != "Go. Guide." {
		t.Errorf("keep.Title = %q, want %q", keep.Title, "Go. Guide.")
	}
}

func TestParseDuckDuckGoJSONAppliesCleaning(t *testing.T) {
	body := `{"Results":[{"Text":"Red   Panda  ...","FirstURL":"https://example.com/a"},{"Text":"Dup","FirstURL":"https://example.com/b"}]}`
	items, err := parseDuckDuckGoJSON([]byte(body), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	// 标题被折叠空白并去除尾部省略号。
	if items[0].Title != "Red Panda" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Red Panda")
	}
	// 摘要与标题重复时被清空。
	if items[1].Snippet != "" {
		t.Errorf("items[1].Snippet = %q, want empty", items[1].Snippet)
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
