package service

import (
	"testing"

	"redpanda/gateway/internal/gateway/model"
)

func TestNormalizeProvider(t *testing.T) {
	tests := map[string]string{
		"":                 "openai_compatible",
		"http_compatible":  "openai_compatible",
		"OPENAI_RESPONSES": "openai_responses",
		" anthropic ":      "anthropic",
	}
	for input, want := range tests {
		got, err := normalizeProvider(input)
		if err != nil || got != want {
			t.Fatalf("normalizeProvider(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := normalizeProvider("unknown"); err == nil {
		t.Fatal("unknown provider should be rejected")
	}
}

func TestNormalizeProviderModels(t *testing.T) {
	models, defaultModel, maxTokens, err := normalizeProviderModels([]ProviderModelInput{
		{Model: " model-a ", Label: "Fast", MaxTokens: 128000, ReasoningEffort: " medium "},
		{Model: "model-b", MaxTokens: 200000, ReasoningEffort: "high"},
	}, "model-b", 0)
	if err != nil {
		t.Fatal(err)
	}
	if defaultModel != "model-b" || maxTokens != 200000 || len(models) != 2 {
		t.Fatalf("normalized models = %#v, default=%q max=%d", models, defaultModel, maxTokens)
	}
	if models[0].Model != "model-a" || models[0].Label != "Fast" || models[0].ReasoningEffort != "medium" {
		t.Fatalf("first model = %#v", models[0])
	}
	if _, _, _, err := normalizeProviderModels([]ProviderModelInput{{Model: "same"}, {Model: "same"}}, "", 0); err == nil {
		t.Fatal("duplicate models should be rejected")
	}
	if _, _, _, err := normalizeProviderModels([]ProviderModelInput{{Model: "m", ReasoningEffort: "turbo"}}, "", 0); err == nil {
		t.Fatal("invalid reasoning effort should be rejected")
	}
}

func TestProviderModelsForReadUpgradesLegacyProfile(t *testing.T) {
	items := providerModelsForRead(model.ProviderProfile{Model: "legacy", MaxTokens: 64000})
	if len(items) != 1 || items[0].Model != "legacy" || items[0].MaxTokens != 64000 {
		t.Fatalf("legacy models = %#v", items)
	}
}
