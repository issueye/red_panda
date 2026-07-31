package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelFacingContentWrapsLegacyAndTruncates(t *testing.T) {
	legacy := Result{
		Name:   "workspace.read_file",
		Status: CallStatusCompleted,
		Output: "project readme",
	}
	facing := ModelFacingContent(legacy)
	var env modelFacingEnvelope
	if err := json.Unmarshal([]byte(facing), &env); err != nil {
		t.Fatal(err)
	}
	if env.Schema != ResultSchemaV1 || env.Tool != "workspace.read_file" || !env.OK {
		t.Fatalf("envelope = %#v", env)
	}
	if env.Text != "project readme" || env.Meta.Note != "legacy output wrapped for model" {
		t.Fatalf("text/meta = %#v", env)
	}
	if env.Data != nil {
		t.Fatalf("legacy result duplicated model content in data: %#v", env.Data)
	}

	big := strings.Repeat("a", maxResultForModel+100)
	standard, _ := json.Marshal(modelFacingEnvelope{
		Schema: ResultSchemaV1,
		Tool:   "workspace.read_file",
		Status: string(CallStatusCompleted),
		OK:     true,
		Text:   big,
		Data:   map[string]any{"content": big},
	})
	truncated := ModelFacingContent(Result{
		Name:   "workspace.read_file",
		Status: CallStatusCompleted,
		Output: string(standard),
	})
	if err := json.Unmarshal([]byte(truncated), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Meta.Truncated {
		t.Fatalf("expected truncation: %#v", env.Meta)
	}
	if len(truncated) > maxResultForModel {
		t.Fatalf("model-facing envelope still too large: %d", len(truncated))
	}
}

func TestModelFacingContentWithLimitBudgetsEntireEnvelope(t *testing.T) {
	const limit = 1024
	standard, _ := json.Marshal(modelFacingEnvelope{
		Schema: ResultSchemaV1,
		Tool:   "workspace.read_file",
		Status: string(CallStatusCompleted),
		OK:     true,
		Text:   strings.Repeat("a", 4000),
		Data:   map[string]any{"content": strings.Repeat("b", 4000)},
	})
	got := ModelFacingContentWithLimit(Result{Name: "workspace.read_file", Status: CallStatusCompleted, Output: string(standard)}, limit)
	if len(got) > limit {
		t.Fatalf("model-facing content = %d bytes, want <= %d", len(got), limit)
	}
	var env modelFacingEnvelope
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Meta.Truncated || env.Data != nil || env.Text == "" {
		t.Fatalf("budgeted envelope = %#v", env)
	}
}

func TestModelFacingContentGoldenLegacyPair(t *testing.T) {
	// Locks the OpenAI-compatible tool message content used by provider golden tests.
	ok := ModelFacingContent(Result{
		ToolCallID: "call_1",
		Name:       "workspace.read_file",
		Status:     CallStatusCompleted,
		Output:     "project readme",
	})
	fail := ModelFacingContent(Result{
		ToolCallID: "call_2",
		Name:       "goal.assess",
		Status:     CallStatusFailed,
		Error:      "not ready",
	})
	wantOK := `{"schema":"red_panda.tool_result.v1","tool":"workspace.read_file","status":"completed","ok":true,"text":"project readme","meta":{"original_bytes":14,"note":"legacy output wrapped for model"}}`
	wantFail := `{"schema":"red_panda.tool_result.v1","tool":"goal.assess","status":"failed","ok":false,"text":"not ready","error":"not ready","meta":{"original_bytes":9,"note":"legacy output wrapped for model"}}`
	if ok != wantOK {
		t.Fatalf("ok content\ngot:  %s\nwant: %s", ok, wantOK)
	}
	if fail != wantFail {
		t.Fatalf("fail content\ngot:  %s\nwant: %s", fail, wantFail)
	}
}
