package runtime

import (
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/tools"
)

func TestCacheDiagnosticsClassifyStableAndChangedPrefixes(t *testing.T) {
	rt := &Runtime{provider: &providerHookRecorder{}, cacheDiagnostics: map[string]cacheDiagnosticState{}}
	request := provider.Request{
		RunID: "run-1", SessionID: "session-1",
		Prompt: provider.PromptEnvelope{
			StablePrefix: []provider.Message{{Role: "system", Content: "stable"}},
			TurnTail:     []provider.Message{{Role: "user", Content: "hello"}},
			CacheEpoch:   "epoch-1",
		},
		Options: provider.RequestOptions{ProviderName: "openai_compatible", Model: "model-a"},
	}
	usage := provider.ProviderUsage{InputTokens: 100}
	if got := rt.diagnoseCacheCall(request, usage, true).missReason; got != "unknown" {
		t.Fatalf("first miss reason = %q", got)
	}

	continued := request
	continued.ToolHistory = []provider.ToolExchange{{
		Call:   tools.Call{ID: "call-1", Name: "workspace.read_file"},
		Result: tools.Result{Output: "done"},
	}}
	if got := rt.diagnoseCacheCall(continued, usage, true).missReason; got != "provider_miss" {
		t.Fatalf("stable continuation miss reason = %q", got)
	}

	changedPrefix := continued
	changedPrefix.Prompt.TurnTail = []provider.Message{{Role: "user", Content: "rewritten"}}
	if got := rt.diagnoseCacheCall(changedPrefix, usage, true).missReason; got != "prefix_changed" {
		t.Fatalf("changed prefix miss reason = %q", got)
	}

	changedEpoch := changedPrefix
	changedEpoch.Prompt.CacheEpoch = "epoch-2"
	if got := rt.diagnoseCacheCall(changedEpoch, usage, true).missReason; got != "epoch_changed" {
		t.Fatalf("changed epoch miss reason = %q", got)
	}
}

func TestCacheDiagnosticsClassifyPolicyAndHit(t *testing.T) {
	rt := &Runtime{provider: &providerHookRecorder{}}
	request := provider.Request{
		RunID: "run-2", SessionID: "session-2",
		Prompt:  provider.PromptEnvelope{CacheEpoch: "epoch"},
		Options: provider.RequestOptions{CacheMode: provider.CacheModeDisabled},
	}
	if got := rt.diagnoseCacheCall(request, provider.ProviderUsage{InputTokens: 100}, true).missReason; got != "unsupported" {
		t.Fatalf("disabled miss reason = %q", got)
	}
	request.SessionID = "session-3"
	request.Options = provider.RequestOptions{MinCacheTokens: 1024}
	if got := rt.diagnoseCacheCall(request, provider.ProviderUsage{InputTokens: 100}, true).missReason; got != "below_minimum" {
		t.Fatalf("minimum miss reason = %q", got)
	}
	request.SessionID = "session-4"
	request.Options = provider.RequestOptions{}
	diagnostic := rt.diagnoseCacheCall(request, provider.ProviderUsage{InputTokens: 100, CacheReadTokens: 80}, true)
	if !diagnostic.cacheHit || diagnostic.missReason != "" || diagnostic.hitRatio != 0.8 {
		t.Fatalf("hit diagnostic = %#v", diagnostic)
	}
}
