package provider

import (
	"io"
	"testing"
)

func TestProviderStreamingDefaultsOnAndCanBeDisabled(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{value: "", want: true},
		{value: "true", want: true},
		{value: "1", want: true},
		{value: "false", want: false},
		{value: "off", want: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("RED_PANDA_PROVIDER_STREAM", test.value)
			if got := providerStreamFromEnv(); got != test.want {
				t.Fatalf("providerStreamFromEnv() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEnvironmentAndProfileProvidersInheritDefaultStreaming(t *testing.T) {
	t.Setenv("RED_PANDA_PROVIDER", "")
	t.Setenv("RED_PANDA_PROVIDER_BASE_URL", "")
	t.Setenv("RED_PANDA_PROVIDER_STREAM", "")

	echo, ok := NewFromEnv(io.Discard).(EchoProvider)
	if !ok || !echo.streamByDefault() {
		t.Fatalf("default provider = %#v, want streaming Echo fallback", echo)
	}
	resolved, ok := Resolve(RequestOptions{
		ProviderName: "openai_compatible", ProviderBaseURL: "https://example.test",
	}, echo.streamByDefault())
	if !ok || !configForProvider(t, resolved).Stream {
		t.Fatalf("profile provider = %#v, want streaming enabled", resolved)
	}
}

func TestExplicitEnvironmentDisablePropagatesToProfileProvider(t *testing.T) {
	t.Setenv("RED_PANDA_PROVIDER", "")
	t.Setenv("RED_PANDA_PROVIDER_BASE_URL", "")
	t.Setenv("RED_PANDA_PROVIDER_STREAM", "false")

	echo := NewFromEnv(io.Discard).(EchoProvider)
	if echo.streamByDefault() {
		t.Fatal("explicit false must disable streaming")
	}
	resolved, ok := Resolve(RequestOptions{
		ProviderName: "anthropic", ProviderBaseURL: "https://example.test",
	}, echo.streamByDefault())
	if !ok || configForProvider(t, resolved).Stream {
		t.Fatalf("profile provider = %#v, want streaming disabled", resolved)
	}
}

func TestResolveIsPublicAliasOfProviderFromOptions(t *testing.T) {
	disable := false
	options := RequestOptions{
		ProviderName: "openai_compatible", ProviderBaseURL: "https://example.test/v1",
		ProviderAPIKey: "k", Model: "m", Stream: &disable,
	}
	a, okA := Resolve(options, true)
	b, okB := providerFromOptions(options, true)
	if !okA || !okB {
		t.Fatalf("ok A=%v B=%v", okA, okB)
	}
	ca, cb := configForProvider(t, a), configForProvider(t, b)
	if ca.BaseURL != cb.BaseURL || ca.APIKey != cb.APIKey || ca.Model != cb.Model || ca.Stream != cb.Stream {
		t.Fatalf("Resolve and providerFromOptions diverged: %#v vs %#v", ca, cb)
	}
	if a.Name() != "openai_compatible" || b.Name() != a.Name() {
		t.Fatalf("names A=%q B=%q", a.Name(), b.Name())
	}
}
