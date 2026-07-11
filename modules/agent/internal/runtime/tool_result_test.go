package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"redpanda/protocol/tools"
)

func TestStandardizeToolOutputWrapsPlainText(t *testing.T) {
	out := standardizeToolOutput("shell.exec", "hello world", nil, 12)
	env, ok := parseStandardToolResult(out)
	if !ok {
		t.Fatalf("expected standard envelope, got %s", out)
	}
	if !env.OK || env.Tool != "shell.exec" || env.Text != "hello world" {
		t.Fatalf("unexpected env: %#v", env)
	}
	if env.Meta.DurationMS != 12 {
		t.Fatalf("duration = %d", env.Meta.DurationMS)
	}
}

func TestStandardizeToolOutputWrapsJSONData(t *testing.T) {
	raw := `{"action":"web.search","answer":"Sunny","items":[{"title":"A","url":"https://a.example","snippet":"s"}]}`
	out := standardizeToolOutput("web.search", raw, nil, 5)
	env, ok := parseStandardToolResult(out)
	if !ok {
		t.Fatalf("expected standard envelope, got %s", out)
	}
	if !strings.Contains(env.Text, "Sunny") {
		t.Fatalf("text should include answer: %q", env.Text)
	}
	// Idempotent: wrapping twice should keep schema.
	out2 := standardizeToolOutput("web.search", out, nil, 9)
	env2, ok := parseStandardToolResult(out2)
	if !ok || env2.Schema != toolResultSchemaV1 {
		t.Fatalf("re-wrap failed: %s", out2)
	}
}

func TestStandardizeToolOutputFailure(t *testing.T) {
	out := standardizeToolOutput("web.fetch", "", fmtError("boom"), 3)
	env, ok := parseStandardToolResult(out)
	if !ok || env.OK || env.Error != "boom" {
		t.Fatalf("expected failed envelope: %#v ok=%v", env, ok)
	}
}

func TestModelFacingToolContentKeepsSchemaAndMarksTruncation(t *testing.T) {
	big := strings.Repeat("a", maxToolResultForModel+100)
	result := tools.Result{
		Name:   "workspace.read_file",
		Status: tools.CallStatusCompleted,
		Output: standardizeToolOutput("workspace.read_file", big, nil, 1),
	}
	facing := modelFacingToolContent(result)
	env, ok := parseStandardToolResult(facing)
	if !ok {
		t.Fatalf("model facing should stay standard: %s", facing)
	}
	if !env.Meta.Truncated {
		t.Fatalf("expected truncated meta: %#v", env.Meta)
	}
	if len(env.Text) > maxToolResultForModel+8 {
		t.Fatalf("text still too large: %d", len(env.Text))
	}
	// Full output on Result is still present for UI.
	if full, ok := parseStandardToolResult(result.Output); !ok || len(full.Text) < maxToolResultForModel {
		t.Fatalf("full result should retain original text length, got %#v", full)
	}
}

func TestReadableToolResultTextFromEnvelope(t *testing.T) {
	out := standardizeToolOutput("web.fetch", `{"action":"web.fetch","title":"T","text":"Body text here"}`, nil, 1)
	text := readableToolResultText(tools.Result{Name: "web.fetch", Output: out, Status: tools.CallStatusCompleted})
	if !strings.Contains(text, "Body text here") {
		t.Fatalf("readable text = %q", text)
	}
}

func TestExtractSearchAnswerFromStandardEnvelope(t *testing.T) {
	rawData, _ := json.Marshal(map[string]any{
		"action": "web.search",
		"answer": "OfficeCLI is a CLI tool.",
		"items": []map[string]any{
			{"title": "Repo", "url": "https://github.com/example", "snippet": "desc"},
		},
	})
	out := standardizeToolOutput("web.search", string(rawData), nil, 1)
	got := extractSearchAnswer(out)
	if !strings.Contains(got, "OfficeCLI is a CLI tool.") || !strings.Contains(got, "https://github.com/example") {
		t.Fatalf("search answer = %q", got)
	}
}

// fmtError avoids importing fmt only for errors in this tiny helper file usage.
func fmtError(msg string) error {
	return errString(msg)
}

type errString string

func (e errString) Error() string { return string(e) }
