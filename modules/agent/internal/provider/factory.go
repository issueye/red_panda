package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"redpanda/protocol/methods"
)

func NewFromEnv(log io.Writer) Provider {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER")))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_BASE_URL")), "/")
	if provider == "openai_compatible" || provider == "http_compatible" || baseURL != "" {
		model := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_MODEL"))
		apiKey := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_API_KEY"))
		if model == "" {
			model = "default"
		}
		if baseURL == "" {
			fmt.Fprintln(log, "provider base url is empty; falling back to echo provider")
			return EchoProvider{}
		}
		return HTTPCompatibleProvider{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   model,
			Stream:  boolEnv("RED_PANDA_PROVIDER_STREAM"),
			Client:  &http.Client{Timeout: 90 * time.Second},
		}
	}
	return EchoProvider{}
}

func providerFromOptions(options methods.ReplyOptions, fallbackStream bool) (HTTPCompatibleProvider, bool) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.ProviderBaseURL), "/")
	if baseURL == "" {
		return HTTPCompatibleProvider{}, false
	}
	provider := strings.ToLower(strings.TrimSpace(options.ProviderName))
	if provider == "" {
		provider = "openai_compatible"
	}
	if provider != "openai_compatible" && provider != "http_compatible" {
		return HTTPCompatibleProvider{}, false
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = "default"
	}
	return HTTPCompatibleProvider{
		BaseURL: baseURL,
		APIKey:  strings.TrimSpace(options.ProviderAPIKey),
		Model:   model,
		Stream:  fallbackStream,
		Client:  &http.Client{Timeout: 90 * time.Second},
	}, true
}

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
