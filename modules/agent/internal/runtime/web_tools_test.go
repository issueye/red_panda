package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// duckDuckGoResultHTML renders a fragment that mirrors the structure of
// https://html.duckduckgo.com/html/ organic results: result__a anchors carry
// the title and a redirect href, result__snippet anchors carry the snippet.
func duckDuckGoResultHTML(title string, target string, snippet string) string {
	return `<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=` + target + `&rut=abc">` + title + `</a>
  <a class="result__snippet" href="#">` + snippet + `</a>
</div>`
}

func TestParseDuckDuckGoHTMLExtractsResultsAndResolvesRedirects(t *testing.T) {
	body := `<html><body>
	<div class="result results_links results_links_deep web-result sponsored result--ad">
	  <a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fad.example.com">Sponsored</a>
	</div>
	` + duckDuckGoResultHTML("Go Official Site", "https%3A%2F%2Fgo.dev", "Go is a programming language.") + `
	` + duckDuckGoResultHTML("Another Result", "https%3A%2F%2Fexample.org%2Fpage", "Second snippet text.") + `
	</body></html>`

	items := parseDuckDuckGoHTML(body, 8)
	if len(items) != 2 {
		t.Fatalf("expected 2 organic results, got %d: %#v", len(items), items)
	}
	if items[0].Title != "Go Official Site" || items[0].URL != "https://go.dev" {
		t.Fatalf("first result mismatch: %#v", items[0])
	}
	if items[0].Snippet != "Go is a programming language." {
		t.Fatalf("first snippet mismatch: %#v", items[0])
	}
	if items[1].URL != "https://example.org/page" {
		t.Fatalf("second url mismatch: %#v", items[1])
	}
	// Sponsored/ad block must be excluded.
	for _, item := range items {
		if strings.Contains(item.URL, "ad.example.com") {
			t.Fatalf("sponsored result leaked into organic results: %#v", item)
		}
	}
}

func TestParseDuckDuckGoHTMLRespectsMaxResults(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < 5; i++ {
		builder.WriteString(duckDuckGoResultHTML("Title "+string(rune('A'+i)), "https%3A%2F%2Fexample.com%2F"+string(rune('a'+i)), "snippet"))
	}
	items := parseDuckDuckGoHTML(builder.String(), 2)
	if len(items) != 2 {
		t.Fatalf("expected 2 results after cap, got %d", len(items))
	}
}

func TestRunWebSearchViaHTTPTestServer(t *testing.T) {
	html := `<html><body>` +
		duckDuckGoResultHTML("First Hit", "https%3A%2F%2Ffirst.example.com", "First snippet.") +
		duckDuckGoResultHTML("Second Hit", "https%3A%2F%2Fsecond.example.com", "Second snippet.") +
		`</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.FormValue("q"); got != "golang testing" {
			t.Fatalf("query mismatch: %q", got)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, html)
	}))
	defer server.Close()
	// Redirect the DDG endpoint at the test server for this test only.
	previous := duckDuckGoHTMLEndpoint
	duckDuckGoHTMLEndpoint = server.URL
	defer func() { duckDuckGoHTMLEndpoint = previous }()

	output, err := runWebSearch(context.Background(), "golang testing", 8, webSearchOptions{Provider: "duckduckgo"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Action string          `json:"action"`
		Count  int             `json:"count"`
		Items  []webSearchItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Action != "web.search" || parsed.Count != 2 {
		t.Fatalf("unexpected payload: %s", output)
	}
	if parsed.Items[0].URL != "https://first.example.com" {
		t.Fatalf("first url mismatch: %#v", parsed.Items[0])
	}
}

func TestRunWebSearchRejectsEmptyQuery(t *testing.T) {
	if _, err := runWebSearch(context.Background(), "   ", 0, webSearchOptions{}); err == nil {
		t.Fatal("empty query should error")
	}
}

func TestRunWebSearchTavily(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tvly-test-key" {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["query"] != "chengdu weather" {
			t.Fatalf("query = %#v", body["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"answer": "Sunny in Chengdu.",
			"results": [
				{"title": "Weather Chengdu", "url": "https://weather.example/chengdu", "content": "31C sunny", "score": 0.91},
				{"title": "China Weather", "url": "https://weather.example/cn", "content": "Southwest forecast", "score": 0.7}
			]
		}`)
	}))
	defer server.Close()
	previous := tavilySearchEndpoint
	tavilySearchEndpoint = server.URL
	defer func() { tavilySearchEndpoint = previous }()

	output, err := runWebSearch(context.Background(), "chengdu weather", 5, webSearchOptions{
		Provider:     "tavily",
		TavilyAPIKey: "tvly-test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Source   string          `json:"source"`
		Provider string          `json:"provider"`
		Answer   string          `json:"answer"`
		Count    int             `json:"count"`
		Items    []webSearchItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Source != "tavily" || parsed.Provider != "tavily" {
		t.Fatalf("unexpected provider/source: %#v", parsed)
	}
	if parsed.Answer != "Sunny in Chengdu." || parsed.Count != 2 {
		t.Fatalf("unexpected payload: %s", output)
	}
	if parsed.Items[0].URL != "https://weather.example/chengdu" || parsed.Items[0].Score != 0.91 {
		t.Fatalf("first item mismatch: %#v", parsed.Items[0])
	}
}

func TestRunWebSearchTavilyRequiresKey(t *testing.T) {
	t.Setenv("RED_PANDA_TAVILY_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	if _, err := runWebSearch(context.Background(), "hello", 3, webSearchOptions{Provider: "tavily"}); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestRunWebSearchAutoPrefersTavilyWhenKeyPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"title":"T","url":"https://t.example","content":"c","score":1}]}`)
	}))
	defer server.Close()
	previous := tavilySearchEndpoint
	tavilySearchEndpoint = server.URL
	defer func() { tavilySearchEndpoint = previous }()

	output, err := runWebSearch(context.Background(), "auto query", 3, webSearchOptions{
		Provider:     "auto",
		TavilyAPIKey: "tvly-auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"source":"tavily"`) {
		t.Fatalf("auto should prefer tavily, got %s", output)
	}
}

func TestEffectiveWebSearchOptions(t *testing.T) {
	opts := effectiveWebSearchOptions(ToolRunContext{
		Reply: &methods.ReplyParams{
			Options: methods.ReplyOptions{
				WebSearchProvider: "tavily",
				WebTavilyAPIKey:   " tvly-from-run ",
				WebHTTPProxy:      "http://127.0.0.1:7890",
			},
		},
	})
	if opts.Provider != "tavily" || opts.TavilyAPIKey != "tvly-from-run" || opts.Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestRunWebFetchExtractsTitleAndText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><head><title>Example Page</title><style>body{color:red}</style></head>
		<body><script>alert("x")</script><p>Hello <b>world</b>.</p><p>Second line.</p></body></html>`)
	}))
	defer server.Close()

	output, err := runWebFetch(context.Background(), server.URL, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Action string `json:"action"`
		Title  string `json:"title"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("fetch should return JSON: %v raw=%s", err, output)
	}
	if parsed.Action != "web.fetch" || parsed.Title != "Example Page" {
		t.Fatalf("unexpected fetch payload: %#v", parsed)
	}
	if !strings.Contains(parsed.Text, "Hello world") || !strings.Contains(parsed.Text, "Second line.") {
		t.Fatalf("missing body text: %q", parsed.Text)
	}
	if strings.Contains(parsed.Text, "alert") || strings.Contains(parsed.Text, "color:red") {
		t.Fatalf("script/style leaked into output: %q", parsed.Text)
	}
}

func TestRunWebOpTimesOut(t *testing.T) {
	started := time.Now()
	_, err := runWebOp(context.Background(), func(ctx context.Context) (string, error) {
		// Block until cancelled.
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not wait much longer than hard timeout (+ small scheduling slack).
	if time.Since(started) > webToolHardTimeout+3*time.Second {
		t.Fatalf("timeout took too long: %s", time.Since(started))
	}
}

func TestRunWebFetchRejectsNonHTTPScheme(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/file", "file:///etc/passwd", "example.com", "javascript:alert(1)"} {
		if _, err := runWebFetch(context.Background(), raw, 0, ""); err == nil {
			t.Fatalf("fetch accepted invalid url %q", raw)
		}
	}
}

func TestRunWebFetchRejectsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	_, err := runWebFetch(context.Background(), server.URL, 0, "")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestParseWebProxyURL(t *testing.T) {
	parsed, err := parseWebProxyURL("127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "http" || parsed.Host != "127.0.0.1:7890" {
		t.Fatalf("unexpected proxy url: %#v", parsed)
	}
	parsed, err = parseWebProxyURL("http://user:pass@proxy.local:8080")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.User.Username() != "user" || parsed.Host != "proxy.local:8080" {
		t.Fatalf("unexpected auth proxy: %#v", parsed)
	}
	parsed, err = parseWebProxyURL("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "socks5" || parsed.Host != "127.0.0.1:1080" {
		t.Fatalf("unexpected socks proxy: %#v", parsed)
	}
	if _, err := parseWebProxyURL(""); err == nil {
		t.Fatal("empty proxy should error")
	}
	if _, err := parseWebProxyURL("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("ftp proxy should be rejected")
	}
}

func TestEffectiveWebHTTPProxyPrefersRunOption(t *testing.T) {
	t.Setenv("RED_PANDA_WEB_HTTP_PROXY", "http://env-proxy:1")
	got := effectiveWebHTTPProxy(ToolRunContext{
		Reply: &methods.ReplyParams{
			Options: methods.ReplyOptions{WebHTTPProxy: " http://run-proxy:7890 "},
		},
	})
	if got != "http://run-proxy:7890" {
		t.Fatalf("got %q, want run option", got)
	}
	got = effectiveWebHTTPProxy(ToolRunContext{})
	if got != "http://env-proxy:1" {
		t.Fatalf("got %q, want env fallback", got)
	}
}

func TestNewWebHTTPClientUsesConfiguredProxy(t *testing.T) {
	client, err := newWebHTTPClient("http://127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("expected transport with proxy function")
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil || proxyURL.Host != "127.0.0.1:7890" {
		t.Fatalf("proxy url = %#v", proxyURL)
	}
}

func TestNewWebHTTPClientRejectsInvalidProxy(t *testing.T) {
	if _, err := newWebHTTPClient("ftp://bad"); err == nil {
		t.Fatal("expected invalid proxy error")
	}
}

func TestAnnotateWebErrorMentionsMissingProxy(t *testing.T) {
	err := annotateWebError(fmt.Errorf(`Post "https://html.duckduckgo.com/html/": net/http: TLS handshake timeout`), "")
	if err == nil || !strings.Contains(err.Error(), "proxy=none") {
		t.Fatalf("expected proxy=none annotation, got %v", err)
	}
	if !strings.Contains(err.Error(), "未使用代理") {
		t.Fatalf("expected Chinese hint, got %v", err)
	}
}

func TestWebToolsRegisteredAsHighRisk(t *testing.T) {
	runner := ToolRunner{}
	for _, name := range []string{"web.search", "web.fetch"} {
		invocation, err := runner.InvocationFromCall("run_web", 0, tools.Call{
			Name:      name,
			Arguments: map[string]any{"query": "x", "url": "https://example.com"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if invocation.Call.Risk != tools.RiskHigh {
			t.Fatalf("%s risk = %s, want high", name, invocation.Call.Risk)
		}
		if invocation.Call.DisplayName == "" {
			t.Fatalf("%s missing display name", name)
		}
	}
}

func TestEffectiveWebOptionsMergePrecedence(t *testing.T) {
	// Default fallback when neither arg nor run option is set.
	if got := effectiveWebResultCount(ToolRunContext{}, 0); got != defaultWebResults {
		t.Fatalf("default result count = %d, want %d", got, defaultWebResults)
	}
	// Explicit tool arg wins over run option.
	withOption := ToolRunContext{Reply: &methods.ReplyParams{Options: methods.ReplyOptions{WebSearchMaxResults: 3}}}
	if got := effectiveWebResultCount(withOption, 5); got != 5 {
		t.Fatalf("arg precedence result count = %d, want 5", got)
	}
	// Run option wins when arg omitted.
	if got := effectiveWebResultCount(withOption, 0); got != 3 {
		t.Fatalf("run option result count = %d, want 3", got)
	}
	// Same precedence for fetch bytes.
	fetchOption := ToolRunContext{Reply: &methods.ReplyParams{Options: methods.ReplyOptions{WebFetchMaxBytes: 1024}}}
	if got := effectiveWebFetchBytes(fetchOption, 0); got != 1024 {
		t.Fatalf("run option fetch bytes = %d, want 1024", got)
	}
	if got := effectiveWebFetchBytes(fetchOption, 4096); got != 4096 {
		t.Fatalf("arg precedence fetch bytes = %d, want 4096", got)
	}
}
