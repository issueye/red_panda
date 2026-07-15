package service

import "testing"

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
