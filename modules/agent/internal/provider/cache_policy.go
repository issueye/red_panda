package provider

import (
	"fmt"
	"strings"
)

const (
	CacheModeImplicit = "implicit"
	CacheModeExplicit = "explicit"
	CacheModeDisabled = "disabled"
)

func effectiveCacheMode(options RequestOptions, providerName string) string {
	mode := strings.ToLower(strings.TrimSpace(options.CacheMode))
	if mode != "" {
		return mode
	}
	if normalizeProviderName(providerName) == "anthropic" {
		return CacheModeExplicit
	}
	return CacheModeImplicit
}

func validateCacheOptions(options RequestOptions) error {
	mode := strings.ToLower(strings.TrimSpace(options.CacheMode))
	switch mode {
	case "", CacheModeImplicit, CacheModeExplicit, CacheModeDisabled:
	default:
		return fmt.Errorf("unsupported provider cache mode %q", options.CacheMode)
	}
	if options.MinCacheTokens < 0 {
		return fmt.Errorf("provider min cache tokens must be >= 0")
	}
	if strings.TrimSpace(options.CacheRetention) != "" && !options.CacheKeySupported {
		return fmt.Errorf("provider cache retention requires cache key support")
	}
	providerName := normalizeProviderName(options.ProviderName)
	if mode == CacheModeExplicit && providerName == "openai_compatible" {
		return fmt.Errorf("explicit cache fields are not supported by generic OpenAI-compatible profiles")
	}
	if mode == CacheModeExplicit && providerName == "openai_responses" && !options.CacheKeySupported {
		return fmt.Errorf("explicit OpenAI Responses caching requires cache key support")
	}
	return nil
}

func explicitCacheEnabled(options RequestOptions, providerName string) bool {
	return effectiveCacheMode(options, providerName) == CacheModeExplicit
}

func responseCacheKeyEnabled(options RequestOptions) bool {
	return effectiveCacheMode(options, "openai_responses") != CacheModeDisabled && options.CacheKeySupported
}
