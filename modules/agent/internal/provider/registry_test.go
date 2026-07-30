package provider

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

type namedProvider string

func (p namedProvider) Name() string { return string(p) }
func (p namedProvider) Complete(context.Context, Request, func(ProviderChunk) error) error {
	return nil
}

func namedFactory(name string) Factory {
	return func(_ RequestOptions, _ bool) (Provider, error) { return namedProvider(name), nil }
}

func TestDefaultRegistryIsDeterministic(t *testing.T) {
	registry := NewDefaultRegistry()
	resolved, err := registry.Resolve(RequestOptions{ProviderBaseURL: "https://example.test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name() != "openai_compatible" {
		t.Fatalf("default provider = %q", resolved.Name())
	}
	want := []string{"anthropic", "http_compatible", "openai_compatible", "openai_responses"}
	if got := registry.Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestRegistryConcurrentRegisterResolveAndRemove(t *testing.T) {
	registry := NewRegistry("custom")
	registry.MustRegister("builtin:provider", "custom", namedFactory("builtin"))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, _ = registry.Register("js:plugin", "custom", namedFactory("plugin"))
		}()
		go func() {
			defer wg.Done()
			_, _ = registry.Resolve(RequestOptions{}, true)
		}()
		go func() {
			defer wg.Done()
			registry.Remove("custom", "js:plugin")
		}()
	}
	wg.Wait()
	registry.Remove("custom", "js:plugin")

	resolved, err := registry.Resolve(RequestOptions{}, true)
	if err != nil || resolved.Name() != "builtin" {
		t.Fatalf("restored provider = %#v, %v", resolved, err)
	}
}

func TestResolveUsesBoundRegistry(t *testing.T) {
	registry := NewRegistry("custom")
	registry.MustRegister("js:test", "custom", namedFactory("custom"))
	resolved, ok := Resolve(WithRegistry(RequestOptions{ProviderName: "custom"}, registry), true)
	if !ok || resolved.Name() != "custom" {
		t.Fatalf("bound resolve = %#v, %v", resolved, ok)
	}
}
