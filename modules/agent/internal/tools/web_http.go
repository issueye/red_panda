package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Shared HTTP client, proxy, and hard-timeout helpers for web tools.
func newWebHTTPClient(proxy string) (*http.Client, error) {
	dialer := &net.Dialer{
		Timeout:   webDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false, // 部分代理上的 HTTP/2 可能永久挂起
		TLSHandshakeTimeout:   webTLSHandshakeTimeout,
		ResponseHeaderTimeout: webResponseHeaderTO,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		// 单次工具调用禁用长连接复用，避免残留半开连接。
		DisableKeepAlives: true,
	}
	if err := applyWebProxy(transport, proxy); err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   webRequestTimeout,
		Transport: transport,
	}, nil
}

// runWebOp 为网络工具设置硬性期限，防止拨号、代理或响应体读取挂起，
// 使工具卡片永久停留在“运行中”。
func runWebOp(ctx context.Context, op func(context.Context) (string, error)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, cancel := context.WithTimeout(ctx, webToolHardTimeout)
	defer cancel()

	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := op(opCtx)
		done <- outcome{out: out, err: err}
	}()

	select {
	case <-opCtx.Done():
		// 若操作错误同时到达，优先返回该错误。
		select {
		case res := <-done:
			if res.err != nil {
				return "", res.err
			}
			return res.out, nil
		default:
			return "", fmt.Errorf("web request timed out after %s", webToolHardTimeout)
		}
	case res := <-done:
		if res.err != nil {
			// 将上下文超时统一为清晰的工具错误。
			if opCtx.Err() != nil && (res.err == context.DeadlineExceeded || strings.Contains(strings.ToLower(res.err.Error()), "deadline exceeded") || strings.Contains(strings.ToLower(res.err.Error()), "context canceled")) {
				return "", fmt.Errorf("web request timed out after %s: %w", webToolHardTimeout, res.err)
			}
			return "", res.err
		}
		return res.out, nil
	}
}

// applyWebProxy 根据显式代理设置配置传输层。
// 空代理保留 ProxyFromEnvironment；无效的显式代理会返回错误，绝不静默忽略。
func applyWebProxy(transport *http.Transport, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := parseWebProxyURL(raw)
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
		return nil
	case "socks5", "socks5h":
		var auth *xproxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &xproxy.Auth{
				User:     parsed.User.Username(),
				Password: password,
			}
		}
		// 使用带超时的基础拨号器，避免 SOCKS 初始化永久挂起。
		base := &net.Dialer{Timeout: webDialTimeout, KeepAlive: 30 * time.Second}
		socksDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, base)
		if err != nil {
			return fmt.Errorf("socks5 dialer: %w", err)
		}
		// SOCKS 拨号器负责连接，因此清除 HTTP 代理以避免双重代理。
		transport.Proxy = nil
		if contextDialer, ok := socksDialer.(xproxy.ContextDialer); ok {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				// 即便父上下文生命周期很长，也限制每次拨号尝试。
				dialCtx, cancel := context.WithTimeout(ctx, webDialTimeout+webTLSHandshakeTimeout)
				defer cancel()
				return contextDialer.DialContext(dialCtx, network, address)
			}
		} else {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				type res struct {
					c   net.Conn
					err error
				}
				ch := make(chan res, 1)
				go func() { c, err := socksDialer.Dial(network, address); ch <- res{c, err} }()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(webDialTimeout + webTLSHandshakeTimeout):
					return nil, fmt.Errorf("socks5 dial timed out")
				case r := <-ch:
					return r.c, r.err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported proxy scheme %q (use http://, https://, or socks5://)", parsed.Scheme)
	}
}

func parseWebProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("proxy is empty")
	}
	// 兼容粘贴时可能出现的全角标点。
	raw = strings.Map(func(r rune) rune {
		switch r {
		case '：':
			return ':'
		case '／':
			return '/'
		default:
			return r
		}
	}, raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		// 支持的协议。
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (use http://, https://, or socks5://)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("proxy host is required")
	}
	return parsed, nil
}

func proxyLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("ALL_PROXY") != "" || os.Getenv("http_proxy") != "" || os.Getenv("https_proxy") != "" {
			return "env"
		}
		return "none"
	}
	parsed, err := parseWebProxyURL(raw)
	if err != nil {
		return "invalid"
	}
	// 绝不将凭据泄露到工具输出或日志。
	return parsed.Scheme + "://" + parsed.Host
}

func annotateWebError(err error, proxy string) error {
	if err == nil {
		return nil
	}
	label := proxyLabel(proxy)
	hint := ""
	msg := strings.ToLower(err.Error())
	switch {
	case label == "none" && (strings.Contains(msg, "tls handshake") || strings.Contains(msg, "timeout")):
		hint = "; 当前未使用代理（proxy=none）。请在设置 → 网络工具填写 HTTP 代理（如 http://127.0.0.1:7890），并确认代理软件已开启"
	case label != "none" && label != "invalid" && (strings.Contains(msg, "connection refused") || strings.Contains(msg, "actively refused") || strings.Contains(msg, "no connection") || strings.Contains(msg, "无法连接代理")):
		hint = "; 无法连接代理 " + label + "，请确认代理软件已启动且端口正确"
	case label != "none" && label != "invalid" && strings.Contains(msg, "tls handshake"):
		hint = "; 已配置代理 " + label + " 但仍 TLS 超时，请检查代理是否可访问目标站点，或改用 socks5://"
	case label == "invalid":
		hint = "; 代理地址无效"
	}
	return fmt.Errorf("%v (proxy=%s)%s", err, label, hint)
}

// ensureWebProxyReachable 在显式代理主机未监听时快速失败。
func ensureWebProxyReachable(ctx context.Context, proxy string) error {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return nil
	}
	parsed, err := parseWebProxyURL(proxy)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return fmt.Errorf("无法连接代理 %s: %w", parsed.Host, err)
	}
	_ = conn.Close()
	return nil
}

// effectiveWebResultCount 合并单次运行选项、显式工具参数和包级默认值，得到结果数量。
// 优先级依次为显式工具参数、桌面端传入的运行选项和默认值。
