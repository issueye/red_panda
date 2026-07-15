package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func NewFromEnv(log io.Writer) Provider {
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
			return EchoProvider{}
		}
		resolved, ok := providerFromOptions(RequestOptions{
			ProviderName: provider, ProviderBaseURL: baseURL,
			ProviderAPIKey: apiKey, Model: model,
		}, boolEnv("RED_PANDA_PROVIDER_STREAM"))
		if ok {
			return resolved
		}
		fmt.Fprintf(log, "unsupported provider %q; falling back to echo provider\n", provider)
	}
	return EchoProvider{}
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
	config := providerConfig{
		BaseURL: baseURL, APIKey: strings.TrimSpace(options.ProviderAPIKey), Model: model,
		Stream: fallbackStream, Client: &http.Client{Timeout: 90 * time.Second},
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

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
