package provider

import (
	"net/http"
	"testing"
)

func TestValidateProxyAcceptsSupportedSchemes(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://127.0.0.1:7890",
		"https://proxy.example",
		"socks5://127.0.0.1:1080",
		"socks5h://127.0.0.1:1080",
		"127.0.0.1:7890",        // 默认补 http://
		"http：//127.0.0.1：7890", // 全角标点归一化
	} {
		if err := ValidateProxy(raw); err != nil {
			t.Fatalf("ValidateProxy(%q) err = %v", raw, err)
		}
	}
}

func TestValidateProxyRejectsBadInput(t *testing.T) {
	for _, raw := range []string{
		"ftp://example",
		"http://",   // 缺 host
		"://broken", // 解析失败
	} {
		if err := ValidateProxy(raw); err == nil {
			t.Fatalf("ValidateProxy(%q) expected error", raw)
		}
	}
}

func TestApplyProxyHTTPSetsTransportProxy(t *testing.T) {
	transport := &http.Transport{}
	if err := applyProxy(transport, "http://127.0.0.1:7890"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	got, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy err = %v", err)
	}
	if got == nil || got.Host != "127.0.0.1:7890" {
		t.Fatalf("expected proxy URL applied, got %#v", got)
	}
}

func TestApplyProxyEmptyLeavesTransportUnchanged(t *testing.T) {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if err := applyProxy(transport, "   "); err != nil {
		t.Fatal(err)
	}
	// 空代理保留原有 Proxy 函数 (这里就是 ProxyFromEnvironment)。
	if transport.Proxy == nil {
		t.Fatal("empty proxy should leave transport.Proxy set")
	}
}

func TestApplyProxyInvalidReturnsError(t *testing.T) {
	transport := &http.Transport{}
	if err := applyProxy(transport, "ftp://nope"); err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

// Resolve 路径必须把代理注入 HTTP 客户端的 transport,且无效代理触发回退。
func TestProviderRegistryAppliesProxyAndFallsBackOnInvalid(t *testing.T) {
	stream := true
	resolved, ok := Resolve(RequestOptions{
		ProviderName:      "openai_compatible",
		ProviderBaseURL:   "https://example.test",
		ProviderHTTPProxy: "http://127.0.0.1:7890",
		Stream:            &stream,
	}, true)
	if !ok {
		t.Fatal("expected provider to resolve")
	}
	cfg := configForProvider(t, resolved)
	transport, ok := cfg.Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", cfg.Client.Transport)
	}
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	applied, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("transport.Proxy err = %v", err)
	}
	if applied == nil || applied.Host != "127.0.0.1:7890" {
		t.Fatalf("expected proxy applied to provider transport, got %#v", applied)
	}

	// 无效代理应让 Resolve 回退到 (nil, false),上层会落到 echo。
	if _, ok := Resolve(RequestOptions{
		ProviderName:      "openai_compatible",
		ProviderBaseURL:   "https://example.test",
		ProviderHTTPProxy: "ftp://nope",
		Stream:            &stream,
	}, true); ok {
		t.Fatal("expected invalid proxy to fail provider resolution")
	}
}
