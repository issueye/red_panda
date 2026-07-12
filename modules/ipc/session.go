package ipc

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Session is a parent-side IPC endpoint: a listener bound to a unique address
// that a child process is expected to dial.
type Session struct {
	Addr     Addr
	Listener net.Listener
}

// ListenTemp creates a Session on a unique temporary path.
// prefix is used in the path name (e.g. "red-panda-agent").
func ListenTemp(prefix string) (*Session, error) {
	addr, err := NewTempAddr(prefix)
	if err != nil {
		return nil, err
	}
	return ListenSession(addr)
}

// ListenSession creates a Session on the given address.
func ListenSession(addr Addr) (*Session, error) {
	ln, err := ListenAddr(addr)
	if err != nil {
		return nil, err
	}
	return &Session{Addr: addr, Listener: ln}, nil
}

// Accept waits for one child connection. If ctx is cancelled or times out,
// Accept returns ctx.Err().
func (s *Session) Accept(ctx context.Context) (net.Conn, error) {
	if s == nil || s.Listener == nil {
		return nil, fmt.Errorf("ipc: session not listening")
	}
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := s.Listener.Accept()
		ch <- result{c, err}
	}()

	select {
	case <-ctx.Done():
		// Unblock Accept by closing the listener. Callers that still need
		// the session must create a new one.
		_ = s.Listener.Close()
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		return r.conn, nil
	}
}

// AcceptTimeout is Accept with a fixed timeout.
func (s *Session) AcceptTimeout(timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return s.Accept(ctx)
}

// Close closes the underlying listener and releases platform resources.
func (s *Session) Close() error {
	if s == nil || s.Listener == nil {
		return nil
	}
	return s.Listener.Close()
}

// DialFromEnv dials the endpoint advertised via EnvAddr.
// This is the usual entry point for child processes.
func DialFromEnv(ctx context.Context) (net.Conn, error) {
	addr, err := AddrFromEnv()
	if err != nil {
		return nil, err
	}
	return DialAddr(ctx, addr)
}
