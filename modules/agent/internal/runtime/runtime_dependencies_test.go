package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	"redpanda/agent/internal/provider"
)

type injectedProvider struct{}

func (injectedProvider) Name() string {
	return "injected"
}

func (injectedProvider) Complete(context.Context, provider.ProviderRequest, func(provider.ProviderChunk) error) error {
	return nil
}

func TestNewWithDependenciesUsesInjectedProvider(t *testing.T) {
	want := injectedProvider{}
	rt := NewWithDependencies(strings.NewReader(""), io.Discard, io.Discard, "test", Dependencies{
		Provider: want,
	})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	if got := rt.provider; got != want {
		t.Fatalf("provider = %#v, want injected provider %#v", got, want)
	}
}

func TestNewWithDependenciesDefaultsProviderFromEnvironment(t *testing.T) {
	t.Setenv("RED_PANDA_PROVIDER", "")
	t.Setenv("RED_PANDA_PROVIDER_BASE_URL", "")

	rt := NewWithDependencies(strings.NewReader(""), io.Discard, io.Discard, "test", Dependencies{})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	if got := rt.provider.Name(); got != "echo" {
		t.Fatalf("provider name = %q, want environment default %q", got, "echo")
	}
}

func TestNewRetainsEnvironmentBackedProviderDefault(t *testing.T) {
	t.Setenv("RED_PANDA_PROVIDER", "")
	t.Setenv("RED_PANDA_PROVIDER_BASE_URL", "")

	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	if got := rt.provider.Name(); got != "echo" {
		t.Fatalf("provider name = %q, want environment default %q", got, "echo")
	}
}

func TestNewWithDependenciesUsesInjectedProviderRegistry(t *testing.T) {
	t.Setenv("RED_PANDA_PROVIDER", "")
	t.Setenv("RED_PANDA_PROVIDER_BASE_URL", "")
	registry := provider.NewRegistry("custom")
	registry.MustRegister("js:test", "custom", func(_ provider.RequestOptions, _ bool) (provider.Provider, error) {
		return injectedProvider{}, nil
	})

	rt := NewWithDependencies(strings.NewReader(""), io.Discard, io.Discard, "test", Dependencies{
		ProviderRegistry: registry,
	})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	if rt.providers != registry {
		t.Fatal("runtime did not retain injected provider registry")
	}
	resolved, ok := provider.Resolve(provider.WithRegistry(provider.RequestOptions{
		ProviderName: "custom",
	}, rt.providers), true)
	if !ok || resolved.Name() != "injected" {
		t.Fatalf("injected registry resolve = %#v, %v", resolved, ok)
	}
}
