package provider

import (
	"reflect"
	"testing"

	"redpanda/protocol/tools"
)

func TestCanonicalToolDefinitionsGolden(t *testing.T) {
	definitions := []tools.Definition{
		{Name: "zeta.run", Description: "Z", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"z": map[string]any{"type": "string", "description": "last"},
				"a": map[string]any{"type": "integer"},
			},
		}},
		{Name: "alpha.run", Description: "A", Parameters: map[string]any{"type": "object"}},
	}

	raw, err := CanonicalJSON(CanonicalToolDefinitions(definitions))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"alpha.run","description":"A","risk":"","parameters":{"type":"object"}},{"name":"zeta.run","description":"Z","risk":"","parameters":{"properties":{"a":{"type":"integer"},"z":{"description":"last","type":"string"}},"type":"object"}}]`
	if string(raw) != want {
		t.Fatalf("canonical tools changed\ngot:  %s\nwant: %s", raw, want)
	}
	if definitions[0].Name != "zeta.run" {
		t.Fatalf("canonicalization mutated caller order: %#v", definitions)
	}
}

func TestComputeCacheEpochOnlyTracksCacheableInputs(t *testing.T) {
	prompt := PromptEnvelope{
		StablePrefix:  []Message{{Role: "system", Content: "policy"}},
		SessionPrefix: []Message{{Role: "system", Content: "skills"}},
		History:       []Message{{Role: "user", Content: "old question"}},
		TurnTail:      []Message{{Role: "user", Content: "current question"}},
	}
	definitions := []tools.Definition{{Name: "workspace.read_file", Parameters: map[string]any{"type": "object"}}}
	base := ComputeCacheEpoch("openai_compatible", "model-a", definitions, prompt)

	dynamic := prompt
	dynamic.History = []Message{{Role: "user", Content: "different history"}}
	dynamic.TurnTail = []Message{{Role: "user", Content: "different turn"}}
	if got := ComputeCacheEpoch("openai_compatible", "model-a", definitions, dynamic); got != base {
		t.Fatalf("dynamic content changed epoch: %q != %q", got, base)
	}

	changedSession := prompt
	changedSession.SessionPrefix = []Message{{Role: "system", Content: "skills-v2"}}
	if got := ComputeCacheEpoch("openai_compatible", "model-a", definitions, changedSession); got == base {
		t.Fatal("session-prefix change did not invalidate epoch")
	}
	if got := ComputeCacheEpoch("openai_compatible", "model-b", definitions, prompt); got == base {
		t.Fatal("model change did not invalidate epoch")
	}
	if got := ComputeCacheEpoch("openai_compatible", "model-a", append(definitions, tools.Definition{Name: "workspace.list"}), prompt); got == base {
		t.Fatal("tool change did not invalidate epoch")
	}
}

func TestComputeScopedCacheKeyIsStablePerSession(t *testing.T) {
	first := ComputeScopedCacheKey("session-a", "epoch-1")
	if first == "" || first != ComputeScopedCacheKey("session-a", "epoch-1") {
		t.Fatalf("scoped key is not stable: %q", first)
	}
	if first == ComputeScopedCacheKey("session-b", "epoch-1") {
		t.Fatal("different sessions shared a cache key")
	}
	if first == ComputeScopedCacheKey("session-a", "epoch-2") {
		t.Fatal("different epochs shared a cache key")
	}
	if got := ComputeScopedCacheKey("", "epoch-1"); got != "epoch-1" {
		t.Fatalf("empty session fallback = %q", got)
	}
}

func TestPromptEnvelopeFlattenMessagesReturnsProtocolOrder(t *testing.T) {
	prompt := PromptEnvelope{
		StablePrefix:  []Message{{Role: "system", Content: "stable"}},
		SessionPrefix: []Message{{Role: "system", Content: "session"}},
		History:       []Message{{Role: "assistant", Content: "history"}},
		TurnTail:      []Message{{Role: "user", Content: "tail"}},
	}
	got := prompt.FlattenMessages()
	want := []Message{
		{Role: "system", Content: "stable"},
		{Role: "system", Content: "session"},
		{Role: "assistant", Content: "history"},
		{Role: "user", Content: "tail"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
	got[0].Content = "mutated"
	if prompt.StablePrefix[0].Content != "stable" {
		t.Fatal("flattened view aliases envelope slice")
	}
}
