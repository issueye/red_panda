package registry

import (
	"fmt"
	"io"
	"sync"

	"redpanda/agent/internal/provider"
)

// ProviderFactory 工厂函数。
type ProviderFactory func(options provider.RequestOptions, log io.Writer) (provider.Provider, error)

// ProviderRegistry Provider 注册表。
type ProviderRegistry struct {
	factories map[string]ProviderFactory
	mu        sync.RWMutex
}

// NewProviderRegistry 创建空 Provider 注册表。
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		factories: make(map[string]ProviderFactory),
	}
}

// Register 注册一个 provider 工厂。
func (r *ProviderRegistry) Register(name string, f ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[name] = f
}

// Resolve 根据 options.ProviderName 解析并创建一个 provider 实例。
// 若 ProviderName 为空则使用第一个注册的工厂；若找不到则返回 error。
func (r *ProviderRegistry) Resolve(options provider.RequestOptions, log io.Writer) (provider.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name := options.ProviderName
	if name == "" {
		for n := range r.factories {
			name = n
			break
		}
		if name == "" {
			return nil, fmt.Errorf("no provider factory registered")
		}
	}

	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("provider factory not found: %q", name)
	}

	return f(options, log)
}

// Names 返回已注册的 provider 名称列表。
func (r *ProviderRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}
