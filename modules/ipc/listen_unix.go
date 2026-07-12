//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// Listen opens a platform IPC listener on path (unix domain socket).
// Any pre-existing socket file at path is removed first.
func Listen(path string) (net.Listener, error) {
	if path == "" {
		return nil, fmt.Errorf("ipc: empty listen path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ipc: mkdir for socket: %w", err)
	}
	// Stale socket files block bind; remove if present.
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		_ = os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("ipc: listen unix %s: %w", path, err)
	}
	// Restrict access to the current user when possible.
	_ = os.Chmod(path, 0o600)
	return &unixListener{Listener: ln, path: path}, nil
}

// ListenAddr opens a listener for the given address.
func ListenAddr(addr Addr) (net.Listener, error) {
	if addr.IsZero() {
		return nil, fmt.Errorf("ipc: empty address")
	}
	if addr.Network != "" && addr.Network != NetworkUnix {
		return nil, fmt.Errorf("ipc: unsupported network %q on unix", addr.Network)
	}
	return Listen(addr.Path)
}

// unixListener removes the socket file on Close.
type unixListener struct {
	net.Listener
	path string
}

func (l *unixListener) Close() error {
	err := l.Listener.Close()
	_ = os.Remove(l.path)
	return err
}
