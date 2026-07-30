package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"redpanda/protocol/pluginmeta"
)

type Factory func(options RequestOptions, fallbackStream bool) (Provider, error)

type FactoryEntry struct {
	Name       string
	Source     string
	Overridden string
	Factory    Factory
}

type FactoryInfo struct {
	Name       string
	Source     string
	Overridden string
}

type Registry struct {
	mu          sync.RWMutex
	entries     map[string][]FactoryEntry
	defaultName string
}

func NewRegistry(defaultName string) *Registry {
	return &Registry{
		entries:     make(map[string][]FactoryEntry),
		defaultName: normalizeProviderName(defaultName),
	}
}

func NewDefaultRegistry() *Registry {
	registry := NewRegistry("openai_compatible")
	registry.MustRegister("builtin:provider", "openai_compatible", newHTTPCompatibleProvider)
	registry.MustRegister("builtin:provider", "http_compatible", newHTTPCompatibleProvider)
	registry.MustRegister("builtin:provider", "openai_responses", newOpenAIResponsesProvider)
	registry.MustRegister("builtin:provider", "anthropic", newAnthropicProvider)
	return registry
}

var defaultRegistry = NewDefaultRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}

func (r *Registry) Register(source, name string, factory Factory) (*FactoryEntry, error) {
	name = normalizeProviderName(name)
	if err := pluginmeta.ValidateProviderName(name); err != nil {
		return nil, err
	}
	if err := pluginmeta.ValidateSource(source); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, fmt.Errorf("provider %q has no factory", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	stack := r.entries[name]
	var previous *FactoryEntry
	if len(stack) > 0 {
		entry := stack[len(stack)-1]
		entry.Overridden = ""
		previous = &entry
	}
	kept := make([]FactoryEntry, 0, len(stack))
	for _, entry := range stack {
		if entry.Source != source {
			kept = append(kept, entry)
		}
	}
	entry := FactoryEntry{Name: name, Source: source, Factory: factory}
	if len(kept) > 0 {
		entry.Overridden = kept[len(kept)-1].Source
	}
	r.entries[name] = append(kept, entry)
	return previous, nil
}

func (r *Registry) MustRegister(source, name string, factory Factory) *FactoryEntry {
	previous, err := r.Register(source, name, factory)
	if err != nil {
		panic(err)
	}
	return previous
}

func (r *Registry) Resolve(options RequestOptions, fallbackStream bool) (Provider, error) {
	name := normalizeProviderName(options.ProviderName)
	r.mu.RLock()
	if name == "" {
		name = r.defaultName
	}
	stack := r.entries[name]
	var factory Factory
	if len(stack) > 0 {
		factory = stack[len(stack)-1].Factory
	}
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("provider factory not found: %q", name)
	}
	options.ProviderName = name
	return factory(options, fallbackStream)
}

func (r *Registry) Remove(name, source string) bool {
	name = normalizeProviderName(name)
	r.mu.Lock()
	defer r.mu.Unlock()

	stack := r.entries[name]
	kept := make([]FactoryEntry, 0, len(stack))
	removed := false
	for _, entry := range stack {
		if entry.Source == source {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false
	}
	if len(kept) == 0 {
		delete(r.entries, name)
	} else {
		r.entries[name] = normalizeFactoryOverrides(kept)
	}
	return true
}

func (r *Registry) List() []FactoryInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]FactoryInfo, 0, len(names))
	for _, name := range names {
		stack := r.entries[name]
		if len(stack) == 0 {
			continue
		}
		entry := stack[len(stack)-1]
		result = append(result, FactoryInfo{Name: name, Source: entry.Source, Overridden: entry.Overridden})
	}
	return result
}

func (r *Registry) Names() []string {
	list := r.List()
	names := make([]string, len(list))
	for i, entry := range list {
		names[i] = entry.Name
	}
	return names
}

func normalizeFactoryOverrides(stack []FactoryEntry) []FactoryEntry {
	for i := range stack {
		stack[i].Overridden = ""
		if i > 0 {
			stack[i].Overridden = stack[i-1].Source
		}
	}
	return stack
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func newHTTPCompatibleProvider(options RequestOptions, fallbackStream bool) (Provider, error) {
	config, err := configFromOptions(options, fallbackStream)
	if err != nil {
		return nil, err
	}
	return HTTPCompatibleProvider{providerConfig: config}, nil
}

func newOpenAIResponsesProvider(options RequestOptions, fallbackStream bool) (Provider, error) {
	config, err := configFromOptions(options, fallbackStream)
	if err != nil {
		return nil, err
	}
	return OpenAIResponsesProvider{providerConfig: config}, nil
}

func newAnthropicProvider(options RequestOptions, fallbackStream bool) (Provider, error) {
	config, err := configFromOptions(options, fallbackStream)
	if err != nil {
		return nil, err
	}
	return AnthropicProvider{providerConfig: config}, nil
}

func configFromOptions(options RequestOptions, fallbackStream bool) (providerConfig, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(options.ProviderBaseURL), "/")
	if baseURL == "" {
		return providerConfig{}, fmt.Errorf("provider base url is empty")
	}
	model := strings.TrimSpace(options.Model)
	if model == "" {
		model = "default"
	}
	stream := fallbackStream
	if options.Stream != nil {
		stream = *options.Stream
	}
	client, err := newProviderHTTPClient(options.ProviderHTTPProxy)
	if err != nil {
		return providerConfig{}, err
	}
	return providerConfig{
		BaseURL: baseURL,
		APIKey:  strings.TrimSpace(options.ProviderAPIKey),
		Model:   model,
		Stream:  stream,
		Client:  client,
	}, nil
}
