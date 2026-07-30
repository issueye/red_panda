package provider

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const providerResponseHeaderTimeout = 90 * time.Second

func NewFromEnv(log io.Writer) Provider {
	return NewFromEnvWithRegistry(DefaultRegistry(), log)
}

func NewFromEnvWithRegistry(registry *Registry, log io.Writer) Provider {
	stream := providerStreamFromEnv()
	providerName := normalizeProviderName(os.Getenv("RED_PANDA_PROVIDER"))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_BASE_URL")), "/")
	if providerName != "" || baseURL != "" {
		model := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_MODEL"))
		apiKey := strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_API_KEY"))
		if model == "" {
			model = "default"
		}
		if baseURL == "" {
			fmt.Fprintln(log, "provider base url is empty; falling back to echo provider")
			return newEchoProvider(stream)
		}
		resolved, err := registry.Resolve(RequestOptions{
			ProviderName: providerName, ProviderBaseURL: baseURL,
			ProviderAPIKey: apiKey, Model: model,
		}, stream)
		if err == nil {
			return resolved
		}
		fmt.Fprintf(log, "provider %q unavailable: %v; falling back to echo provider\n", providerName, err)
	}
	return newEchoProvider(stream)
}

// Resolve builds a concrete provider from per-run options (profile override).
// Environment defaults and per-run profile selection share this resolver
// (docs/39 Wave 2). fallbackStream is used when options.Stream is nil.
func Resolve(options RequestOptions, fallbackStream bool) (Provider, bool) {
	registry := options.registry
	if registry == nil {
		registry = DefaultRegistry()
	}
	resolved, err := registry.Resolve(options, fallbackStream)
	if err != nil {
		return nil, false
	}
	return resolved, true
}

// newProviderHTTPClient 构造 provider 出站 HTTP 客户端。
// proxyURL 为空时保留默认的 ProxyFromEnvironment 行为;非空时按 scheme 应用代理。
// 无效代理会返回错误,绝不静默回退,避免配置错误被掩盖。
func newProviderHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = providerResponseHeaderTimeout
	// 保留 DefaultTransport 的 HTTP/2 协商(ALPN h2)。不要照搬 web 工具的
	// ForceAttemptHTTP2=false:web 工具用全新的 &http.Transport{}(不 advertise
	// h2,干净地说 HTTP/1.1);而这里 Clone 自 DefaultTransport,TLS NextProtos
	// 仍含 ["h2","http/1.1"]。若只把 ForceAttemptHTTP2 关掉,握手仍会协商出 h2,
	// 但 Go 走 HTTP/1.x 解析器去读 HTTP/2 帧,导致 "malformed HTTP response"。
	if err := applyProxy(transport, proxyURL); err != nil {
		return nil, err
	}
	// Client.Timeout also covers response-body reads, so it cannot be used for
	// long-lived streaming responses. Request contexts still provide cancellation.
	return &http.Client{Transport: transport}, nil
}

func providerStreamFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_PROVIDER_STREAM"))) {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}
