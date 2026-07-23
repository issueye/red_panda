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
	if len(env.Text) > maxResultForModel+8 {
		t.Fatalf("text still too large: %d", len(env.Text))
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
	wantOK := `{"schema":"red_panda.tool_result.v1","tool":"workspace.read_file","status":"completed","ok":true,"text":"project readme","data":{"content":"project readme"},"meta":{"original_bytes":14,"note":"legacy output wrapped for model"}}`
	wantFail := `{"schema":"red_panda.tool_result.v1","tool":"goal.assess","status":"failed","ok":false,"text":"not ready","data":{"content":"not ready"},"error":"not ready","meta":{"original_bytes":9,"note":"legacy output wrapped for model"}}`
	if ok != wantOK {
		t.Fatalf("ok content\ngot:  %s\nwant: %s", ok, wantOK)
	}
	if fail != wantFail {
		t.Fatalf("fail content\ngot:  %s\nwant: %s", fail, wantFail)
	}
}
