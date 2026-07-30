package registry

import "redpanda/agent/internal/provider"

type ProviderFactory = provider.Factory
type ProviderRegistry = provider.Registry

func NewProviderRegistry() *ProviderRegistry {
	return provider.NewRegistry("openai_compatible")
}
