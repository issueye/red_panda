package provider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// 拨号与 TLS 握手超时,避免代理初始化或首次握手永久挂起。
const (
	proxyDialTimeout         = 15 * time.Second
	proxyTLSHandshakeTimeout = 15 * time.Second
)

// ValidateProxy 校验可选的代理 URL。空串合法(表示回退到环境代理)。
// 不执行任何网络探测,仅做语法检查。
func ValidateProxy(raw string) error {
	_, err := parseProxyURL(raw)
	return err
}

// applyProxy 根据显式代理设置配置传输层。
// 空代理保留 ProxyFromEnvironment;无效的显式代理会返回错误,绝不静默忽略。
func applyProxy(transport *http.Transport, raw string) error {
	parsed, err := parseProxyURL(raw)
	if err != nil {
		return err
	}
	if parsed == nil {
		return nil
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
		// 使用带超时的基础拨号器,避免 SOCKS 初始化永久挂起。
		base := &net.Dialer{Timeout: proxyDialTimeout, KeepAlive: 30 * time.Second}
		socksDialer, err := xproxy.SOCKS5("tcp", parsed.Host, auth, base)
		if err != nil {
			return fmt.Errorf("socks5 dialer: %w", err)
		}
		// SOCKS 拨号器负责连接,因此清除 HTTP 代理以避免双重代理。
		transport.Proxy = nil
		if contextDialer, ok := socksDialer.(xproxy.ContextDialer); ok {
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				// 即便父上下文生命周期很长,也限制每次拨号尝试。
				dialCtx, cancel := context.WithTimeout(ctx, proxyDialTimeout+proxyTLSHandshakeTimeout)
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
				case <-time.After(proxyDialTimeout + proxyTLSHandshakeTimeout):
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

// parseProxyURL 解析并归一化代理 URL。空串返回 (nil, nil)。
func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
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
