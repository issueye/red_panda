//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Dial connects to a unix-domain socket at path.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		return nil, fmt.Errorf("ipc: empty dial path")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ipc: dial unix %s: %w", path, err)
	}
	return conn, nil
}

// DialAddr connects to the given address.
func DialAddr(ctx context.Context, addr Addr) (net.Conn, error) {
	if addr.IsZero() {
		return nil, fmt.Errorf("ipc: empty address")
	}
	if addr.Network != "" && addr.Network != NetworkUnix {
		return nil, fmt.Errorf("ipc: unsupported network %q on unix", addr.Network)
	}
	return Dial(ctx, addr.Path)
}

// DialTimeout is a convenience wrapper around Dial with a timeout.
func DialTimeout(path string, timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return Dial(ctx, path)
}
