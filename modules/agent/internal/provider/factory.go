package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const providerResponseHeaderTimeout = 90 * time.Second

func NewFromEnv(log io.Writer) Provider {
	stream := providerStreamFromEnv()
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER")))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_BASE_URL")), "/")
	if provider != "" || baseURL != "" {
		model := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_MODEL"))
		apiKey := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_API_KEY"))
		if model == "" {
			model = "default"
		}
		if baseURL == "" {
			fmt.Fprintln(log, "provider base url is empty; falling back to echo provider")
			return newEchoProvider(stream)
		}
		resolved, ok := Resolve(RequestOptions{
			ProviderName: provider, ProviderBaseURL: baseURL,
			ProviderAPIKey: apiKey, Model: model,
		}, stream)
		if ok {
			return resolved
		}
		fmt.Fprintf(log, "unsupported provider %q; falling back to echo provider\n", provider)
	}
	return newEchoProvider(stream)
}

// Resolve builds a concrete provider from per-run options (profile override).
// Environment defaults and per-run profile selection share this resolver
// (docs/39 Wave 2). fallbackStream is used when options.Stream is nil.
func Resolve(options RequestOptions, fallbackStream bool) (Provider, bool) {
	return providerFromOptions(options, fallbackStream)
}

func providerFromOptions(options RequestOptions, fallbackStream bool) (Provider, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.ProviderBaseURL), "/")
	if baseURL == "" {
		return nil, false
	}
	provider := strings.ToLower(strings.TrimSpace(options.ProviderName))
	if provider == "" {
		provider = "openai_compatible"
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = "default"
	}
	stream := fallbackStream
	if options.Stream != nil {
		stream = *options.Stream
	}
	config := providerConfig{
		BaseURL: baseURL, APIKey: strings.TrimSpace(options.ProviderAPIKey), Model: model,
		Stream: stream, Client: newProviderHTTPClient(),
	}
	switch provider {
	case "openai_compatible", "http_compatible":
		return HTTPCompatibleProvider{providerConfig: config}, true
	case "openai_responses":
		return OpenAIResponsesProvider{providerConfig: config}, true
	case "anthropic":
		return AnthropicProvider{providerConfig: config}, true
	default:
		return nil, false
	}
}

func newProviderHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = providerResponseHeaderTimeout
	// Client.Timeout also covers response-body reads, so it cannot be used for
	// long-lived streaming responses. Request contexts still provide cancellation.
	return &http.Client{Transport: transport}
}

func providerStreamFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_STREAM"))) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}
