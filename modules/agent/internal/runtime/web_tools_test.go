package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	output, err := runWebSearch(context.Background(), "golang testing", 8)
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
	if _, err := runWebSearch(context.Background(), "   ", 0); err == nil {
		t.Fatal("empty query should error")
	}
}

func TestRunWebFetchExtractsTitleAndText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><head><title>Example Page</title><style>body{color:red}</style></head>
		<body><script>alert("x")</script><p>Hello <b>world</b>.</p><p>Second line.</p></body></html>`)
	}))
	defer server.Close()

	output, err := runWebFetch(context.Background(), server.URL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output, "Example Page") {
		t.Fatalf("missing title prefix: %q", output)
	}
	if !strings.Contains(output, "Hello world") || !strings.Contains(output, "Second line.") {
		t.Fatalf("missing body text: %q", output)
	}
	if strings.Contains(output, "alert") || strings.Contains(output, "color:red") {
		t.Fatalf("script/style leaked into output: %q", output)
	}
}

func TestRunWebFetchRejectsNonHTTPScheme(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/file", "file:///etc/passwd", "example.com", "javascript:alert(1)"} {
		if _, err := runWebFetch(context.Background(), raw, 0); err == nil {
			t.Fatalf("fetch accepted invalid url %q", raw)
		}
	}
}

func TestRunWebFetchRejectsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	_, err := runWebFetch(context.Background(), server.URL, 0)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
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
