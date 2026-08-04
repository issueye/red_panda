// Package runtimeclient is the Gateway-side client for the Agent Runtime
// subprocess. It owns process spawn (stdio/IPC), the JSON-RPC wire framing,
// per-run subprocess management, and the public RPC surface consumed by
// services.
//
// File layout (docs/plans/2026-07-19-convergence-wave.md Wave C Task C1):
//   - client.go          — Client struct, constructors, Status, env helpers,
//                          call framing, stdout/stderr/wait I/O loop.
//   - client_run.go      — Run lifecycle RPCs + per-run subprocess management.
//   - client_worker.go   — Worker and permission RPCs.
//   - client_mgmt.go     — MCP discovery and skill catalog RPCs.
//   - client_transport.go — ensureStarted dispatcher, stdio/IPC spawn, Shutdown.
package runtimeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

type EventHandler func(events.EnvelopeV2)

type RequestHandler func(context.Context, string, json.RawMessage) (any, error)

type RunExitHandler func(runID string, err error)

const maxRuntimeJSONRPCLineBytes = 4 * 1024 * 1024

type Client struct {
	command   string
	args      []string
	version   string
	onEvent   EventHandler
	onRequest RequestHandler
	onRunExit RunExitHandler
	onExit    func(error)

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	ipcCloser io.Closer // session listener when transport is IPC
	pending   map[jsonrpc.ID]chan jsonrpc.Response
	nextID    uint64
	running   bool
	perRuns   map[string]*Client
	transport string // "stdio" or "ipc"
	// extraEnv are KEY=VALUE entries injected into the child agent process environment.
	// Used for per-run settings such as worker pool size.
	extraEnv []string
}

// setWorkerPoolSize stores a desired pool size (1-8) to be injected as
// RED_PANDA_WORKER_POOL_SIZE into future child agent processes.
// Zero or out-of-range values are ignored (Runtime will use its default).
func (c *Client) setWorkerPoolSize(size int) {
	if size < 1 || size > 8 {
		return
	}
	c.setExtraEnv("RED_PANDA_WORKER_POOL_SIZE", fmt.Sprintf("%d", size))
}

// SetSkillsRoot exposes the Gateway-owned built-in skill directory to Agent
// processes. Workspace write tools remain scoped to their working directory.
func (c *Client) SetSkillsRoot(root string) {
	c.setExtraEnv(methods.EnvSkillsDir, strings.TrimSpace(root))
}

func (c *Client) setExtraEnv(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	filtered := make([]string, 0, len(c.extraEnv))
	for _, e := range c.extraEnv {
		if !strings.HasPrefix(e, key+"=") {
			filtered = append(filtered, e)
		}
	}
	if value != "" {
		filtered = append(filtered, key+"="+value)
	}
	c.extraEnv = filtered
}

// buildChildEnv merges base environment with extra KEY=VALUE entries,
// ensuring later entries override earlier ones for the same key.
func buildChildEnv(base []string, extra []string) []string {
	out := make([]string, len(base))
	copy(out, base)
	for _, e := range extra {
		key := strings.SplitN(e, "=", 2)[0]
		prefix := key + "="
		replaced := false
		for i := range out {
			if strings.HasPrefix(out[i], prefix) {
				out[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, e)
		}
	}
	return out
}

func New(command string, args []string, version string, onEvent EventHandler, onRequest RequestHandler) *Client {
	return &Client{
		command:   command,
		args:      args,
		version:   version,
		onEvent:   onEvent,
		onRequest: onRequest,
		pending:   map[jsonrpc.ID]chan jsonrpc.Response{},
		perRuns:   map[string]*Client{},
	}
}

// SetRunExitHandler reports dedicated run processes that stop before emitting
// a terminal event. The gateway uses this to release persisted concurrency slots.
func (c *Client) SetRunExitHandler(handler RunExitHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRunExit = handler
}

func (c *Client) Status() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	activePerRuns := len(c.perRuns)
	mode := "single_core"
	switch {
	case activePerRuns > 0 && c.running:
		mode = "mixed"
	case activePerRuns > 0:
		mode = "per_run_process"
	}
	transport := c.transport
	if transport == "" {
		transport = runtimeTransport()
	}
	return map[string]any{
		"available":       c.running || activePerRuns > 0,
		"mode":            mode,
		"transport":       transport,
		"command":         c.command,
		"active_per_runs": activePerRuns,
		"core_running":    c.running,
	}
}

func normalizedRuntimeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "per_run_process":
		return "per_run_process"
	case "single_core":
		return "single_core"
	default:
		// Keep client-level default aligned with Gateway multi-session isolation default.
		return "per_run_process"
	}
}

// call writes a JSON-RPC request and waits for the matching response (or
// timeout / ctx cancel). The 30s ceiling protects callers from a stuck runtime.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextRequestID()
	req, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan jsonrpc.Response, 1)
	c.mu.Lock()
	if !c.running || c.stdin == nil {
		c.mu.Unlock()
		return nil, errors.New("runtime process is not running")
	}
	c.pending[id] = ch
	_, err = fmt.Fprintln(c.stdin, string(raw))
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, err
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-timer.C:
		c.removePending(id)
		return nil, fmt.Errorf("runtime request %s timed out", method)
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("runtime process stopped")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("runtime error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *Client) nextRequestID() jsonrpc.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return jsonrpc.ID(fmt.Sprintf("gw_%d", c.nextID))
}

func (c *Client) removePending(id jsonrpc.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *Client) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxRuntimeJSONRPCLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		var probe struct {
			ID     jsonrpc.ID `json:"id"`
			Method string     `json:"method"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.Method != "" && probe.ID != "" {
			c.handleRequest(line)
			continue
		}
		if probe.Method != "" {
			c.handleNotification(line, probe.Method)
			continue
		}
		var resp jsonrpc.Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

func (c *Client) handleRequest(line []byte) {
	var req jsonrpc.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	if c.onRequest == nil {
		_ = c.writeRuntimeResponse(jsonrpc.NewError(req.ID, -32601, "method not found"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := c.onRequest(ctx, req.Method, req.Params)
	if err != nil {
		_ = c.writeRuntimeResponse(jsonrpc.NewError(req.ID, -32050, err.Error()))
		return
	}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		_ = c.writeRuntimeResponse(jsonrpc.NewError(req.ID, -32603, "internal error"))
		return
	}
	_ = c.writeRuntimeResponse(resp)
}

func (c *Client) writeRuntimeResponse(resp jsonrpc.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return errors.New("runtime process is not running")
	}
	_, err = fmt.Fprintln(c.stdin, string(raw))
	return err
}

func (c *Client) handleNotification(line []byte, method string) {
	if method != methods.RunEvent || c.onEvent == nil {
		return
	}
	var note jsonrpc.Notification
	if err := json.Unmarshal(line, &note); err != nil {
		return
	}
	var event events.EnvelopeV2
	if err := json.Unmarshal(note.Params, &event); err != nil {
		return
	}
	c.onEvent(event)
}

func (c *Client) drainStderr(stderr io.Reader) {
	_, _ = io.Copy(io.Discard, stderr)
}

func (c *Client) wait(cmd *exec.Cmd) {
	err := cmd.Wait()
	c.mu.Lock()
	var onExit func(error)
	if c.cmd == cmd {
		c.running = false
		c.cmd = nil
		c.stdin = nil
		if c.ipcCloser != nil {
			_ = c.ipcCloser.Close()
			c.ipcCloser = nil
		}
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		onExit = c.onExit
	}
	c.mu.Unlock()
	if onExit != nil {
		onExit(err)
	}
}
