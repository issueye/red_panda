package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultAcceptTimeout is used by ServeProcess when no timeout is set.
const DefaultAcceptTimeout = 15 * time.Second

// ProcessConfig configures a child process that will dial back into a
// parent-owned IPC session (replacing stdio pipes).
type ProcessConfig struct {
	// Command is the executable path.
	Command string
	// Args are extra arguments (not including Command).
	Args []string
	// Env is additional environment entries (KEY=VALUE). Parent environ is
	// always inherited; EnvAddr is set automatically from the session.
	Env []string
	// Dir is the child working directory. Empty keeps the parent directory.
	Dir string
	// Prefix used for the temporary endpoint name.
	Prefix string
	// AcceptTimeout bounds how long the parent waits for the child to dial.
	// Zero selects DefaultAcceptTimeout.
	AcceptTimeout time.Duration
	// Extra setup applied to the exec.Cmd before Start (optional).
	// Stderr can be redirected here; stdin/stdout are left unused for IPC.
	Configure func(*exec.Cmd)
}

// Process is a running child bound to an IPC connection.
type Process struct {
	Cmd     *exec.Cmd
	Session *Session
	Conn    net.Conn
	Stream  *Stream
}

// StartProcess listens on a temp IPC endpoint, starts the child with
// EnvAddr set, and accepts the dial. On success the returned Process owns
// the connection; call Close to tear everything down.
//
// This is the recommended parent-side replacement for:
//
//	cmd.StdinPipe() / cmd.StdoutPipe() + newline JSON-RPC.
func StartProcess(ctx context.Context, cfg ProcessConfig) (*Process, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("ipc: empty command")
	}
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "red-panda-ipc"
	}
	session, err := ListenTemp(prefix)
	if err != nil {
		return nil, err
	}

	// Use Command (not CommandContext): the child is a long-lived peer and must
	// not die when the caller's short-lived request context is cancelled.
	// ctx only bounds Accept below.
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	// Drop any inherited parent endpoint so the child only dials this session.
	cmd.Env = append(stripEnvKey(os.Environ(), EnvAddr), cfg.Env...)
	cmd.Env = append(cmd.Env, ChildEnv(session.Addr)...)
	// Detach from parent stdio for protocol traffic; leave stderr for logs
	// unless Configure overrides it.
	cmd.Stdin = nil
	cmd.Stdout = nil
	if cfg.Configure != nil {
		cfg.Configure(cmd)
	}

	if err := cmd.Start(); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("ipc: start process: %w", err)
	}

	timeout := cfg.AcceptTimeout
	if timeout <= 0 {
		timeout = DefaultAcceptTimeout
	}
	acceptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := session.Accept(acceptCtx)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_ = session.Close()
		return nil, fmt.Errorf("ipc: accept child connection: %w", err)
	}

	return &Process{
		Cmd:     cmd,
		Session: session,
		Conn:    conn,
		Stream:  NewStream(conn, DefaultMaxLineBytes),
	}, nil
}

// stripEnvKey removes KEY=... entries from an environ slice.
func stripEnvKey(environ []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environ))
	for _, e := range environ {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Close closes the stream/connection and session, then waits for the child
// to exit (best-effort kill if still running after a short grace period).
func (p *Process) Close() error {
	if p == nil {
		return nil
	}
	var first error
	if p.Stream != nil {
		if err := p.Stream.Close(); err != nil && first == nil {
			first = err
		}
	} else if p.Conn != nil {
		if err := p.Conn.Close(); err != nil && first == nil {
			first = err
		}
	}
	if p.Session != nil {
		if err := p.Session.Close(); err != nil && first == nil {
			first = err
		}
	}
	if p.Cmd != nil && p.Cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- p.Cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = p.Cmd.Process.Kill()
			<-done
		}
	}
	return first
}
