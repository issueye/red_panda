package registry

import (
	"context"
	"io"
	"testing"

	"redpanda/agent/internal/provider"
)

type testProvider struct {
	name string
}

func (p *testProvider) Name() string             { return p.name }
func (p *testProvider) Complete(_ context.Context, _ provider.Request, _ func(provider.ProviderChunk) error) error {
	return nil
}

func TestProviderRegistryResolve(t *testing.T) {
	reg := NewProviderRegistry()
	reg.Register("openai_compatible", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) {
		return &testProvider{name: "openai_compatible"}, nil
	})
	reg.Register("openai_responses", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) {
		return &testProvider{name: "openai_responses"}, nil
	})
	reg.Register("anthropic", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) {
		return &testProvider{name: "anthropic"}, nil
	})

	// 已知 name
	for _, want := range []string{"openai_compatible", "openai_responses", "anthropic"} {
		p, err := reg.Resolve(provider.RequestOptions{ProviderName: want}, io.Discard)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", want, err)
		}
		if p.Name() != want {
			t.Fatalf("Provider name = %q, want %q", p.Name(), want)
		}
	}

	// 未知 name
	_, err := reg.Resolve(provider.RequestOptions{ProviderName: "unknown"}, io.Discard)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}

	// 空 name 应回退到第一个注册的工厂
	p, err := reg.Resolve(provider.RequestOptions{}, io.Discard)
	if err != nil {
		t.Fatalf("Resolve with empty name: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider fallback")
	}
}

func TestProviderRegistryNames(t *testing.T) {
	reg := NewProviderRegistry()
	reg.Register("a", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) { return nil, nil })
	reg.Register("b", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) { return nil, nil })
	reg.Register("c", func(_ provider.RequestOptions, _ io.Writer) (provider.Provider, error) { return nil, nil })

	names := reg.Names()
	if len(names) != 3 {
		t.Fatalf("names count = %d, want 3", len(names))
	}
}
