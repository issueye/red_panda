package runtimeclient

// Transport lifecycle: stdio/IPC spawn, ensureStarted dispatcher, Shutdown.
// Receivers stay on Client; this file only relocates them by domain
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C1).

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"

	"redpanda/ipc"
	"redpanda/protocol/methods"
)

func (c *Client) ensureStarted(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if runtimeTransport() == "ipc" {
		return c.ensureStartedIPC(ctx)
	}
	return c.ensureStartedStdio()
}

// runtimeTransport selects the gateway↔agent wire transport (docs/41 W3-2).
// Default is IPC (Windows named pipes / Unix domain sockets).
// Set RED_PANDA_RUNTIME_IPC=0 (or "false"/"stdio") to force legacy stdio escape hatch.
func runtimeTransport() string {
	v := strings.TrimSpace(os.Getenv("RED_PANDA_RUNTIME_IPC"))
	switch strings.ToLower(v) {
	case "0", "false", "no", "stdio", "off":
		return "stdio"
	default:
		return "ipc"
	}
}

func (c *Client) ensureStartedStdio() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}

	log.Printf("runtimeclient: using legacy stdio transport (set RED_PANDA_RUNTIME_IPC unset/1 for default IPC)")

	cmd := exec.Command(c.command, c.args...)
	// Inject any extra environment (e.g. RED_PANDA_WORKER_POOL_SIZE) for this agent process.
	if len(c.extraEnv) > 0 {
		cmd.Env = buildChildEnv(os.Environ(), c.extraEnv)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return err
	}

	c.cmd = cmd
	c.stdin = stdin
	c.transport = "stdio"
	c.running = true
	c.mu.Unlock()

	go c.readStdout(stdout)
	go c.drainStderr(stderr)
	go c.wait(cmd)
	return nil
}

func (c *Client) ensureStartedIPC(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return err
	}

	extra := c.extraEnv
	proc, err := ipc.StartProcess(ctx, ipc.ProcessConfig{
		Command: c.command,
		Args:    c.args,
		Prefix:  "red-panda-agent",
		Env:     extra,
		Configure: func(cmd *exec.Cmd) {
			cmd.Stderr = stderrWriter
		},
	})
	if err != nil {
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return err
	}
	// Parent keeps the read end; child holds the write end via inheritance.
	_ = stderrWriter.Close()

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		_ = proc.Close()
		_ = stderrReader.Close()
		return nil
	}
	c.cmd = proc.Cmd
	// net.Conn is ReadWriteCloser; gateway writes requests/responses on the
	// same stream it reads agent events from (replaces stdin+stdout pair).
	c.stdin = proc.Conn
	c.ipcCloser = proc.Session
	c.transport = "ipc"
	c.running = true
	c.mu.Unlock()

	go c.readStdout(proc.Conn)
	go c.drainStderr(stderrReader)
	go c.wait(proc.Cmd)
	return nil
}

// Shutdown closes all per-run children, sends core.shutdown to the shared core,
// then closes stdin/IPC and kills the process.
func (c *Client) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	perRuns := make([]*Client, 0, len(c.perRuns))
	for runID, child := range c.perRuns {
		perRuns = append(perRuns, child)
		delete(c.perRuns, runID)
	}
	c.mu.Unlock()
	for _, child := range perRuns {
		_ = child.Shutdown(ctx)
	}

	c.mu.Lock()
	if !c.running || c.cmd == nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	_, _ = c.call(ctx, methods.CoreShutdown, map[string]any{})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	if c.ipcCloser != nil {
		_ = c.ipcCloser.Close()
		c.ipcCloser = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.running = false
	return nil
}
