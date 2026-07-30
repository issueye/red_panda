package registry

import (
	"context"
	"testing"

	"redpanda/agent/internal/provider"
)

type testProvider struct {
	name string
}

func (p *testProvider) Name() string { return p.name }
func (p *testProvider) Complete(_ context.Context, _ provider.Request, _ func(provider.ProviderChunk) error) error {
	return nil
}

func TestProviderRegistryResolve(t *testing.T) {
	reg := NewProviderRegistry()
	reg.Register("builtin:test", "openai_compatible", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) {
		return &testProvider{name: "openai_compatible"}, nil
	})
	reg.Register("builtin:test", "openai_responses", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) {
		return &testProvider{name: "openai_responses"}, nil
	})
	reg.Register("builtin:test", "anthropic", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) {
		return &testProvider{name: "anthropic"}, nil
	})

	// 已知 name
	for _, want := range []string{"openai_compatible", "openai_responses", "anthropic"} {
		p, err := reg.Resolve(provider.RequestOptions{ProviderName: want}, true)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", want, err)
		}
		if p.Name() != want {
			t.Fatalf("Provider name = %q, want %q", p.Name(), want)
		}
	}

	// 未知 name
	_, err := reg.Resolve(provider.RequestOptions{ProviderName: "unknown"}, true)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}

	// 空 name 应回退到第一个注册的工厂
	p, err := reg.Resolve(provider.RequestOptions{}, true)
	if err != nil {
		t.Fatalf("Resolve with empty name: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider fallback")
	}
}

func TestProviderRegistryNames(t *testing.T) {
	reg := NewProviderRegistry()
	reg.Register("builtin:test", "a", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) { return nil, nil })
	reg.Register("builtin:test", "b", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) { return nil, nil })
	reg.Register("builtin:test", "c", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) { return nil, nil })

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("names count = %d, want 3", len(names))
	}
}

func TestProviderRegistryOverrideAndRestore(t *testing.T) {
	reg := NewProviderRegistry()
	factory := func(name string) provider.Factory {
		return func(_ provider.RequestOptions, _ bool) (provider.Provider, error) {
			return &testProvider{name: name}, nil
		}
	}
	reg.MustRegister("builtin:provider", "openai_compatible", factory("builtin"))
	reg.MustRegister("js:plugin", "openai_compatible", factory("plugin"))

	resolved, err := reg.Resolve(provider.RequestOptions{}, true)
	if err != nil || resolved.Name() != "plugin" {
		t.Fatalf("plugin resolve = %#v, %v", resolved, err)
	}
	if !reg.Remove("openai_compatible", "js:plugin") {
		t.Fatal("expected plugin removal")
	}
	resolved, err = reg.Resolve(provider.RequestOptions{}, true)
	if err != nil || resolved.Name() != "builtin" {
		t.Fatalf("restored resolve = %#v, %v", resolved, err)
	}
}
