package runtimeclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"redpanda/ipc"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	protocolmcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
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
	c.mu.Lock()
	defer c.mu.Unlock()
	// Replace any previous setting for this key.
	filtered := make([]string, 0, len(c.extraEnv))
	for _, e := range c.extraEnv {
		if !strings.HasPrefix(e, "RED_PANDA_WORKER_POOL_SIZE=") {
			filtered = append(filtered, e)
		}
	}
	filtered = append(filtered, fmt.Sprintf("RED_PANDA_WORKER_POOL_SIZE=%d", size))
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

func (c *Client) Initialize(ctx context.Context) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	raw, err := c.call(ctx, methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersionV2,
		Client: methods.PeerInfo{
			Name:    "red-panda-gateway",
			Version: c.version,
		},
		Environment: methods.Environment{
			PermissionMode: "strict",
		},
		Capabilities: []methods.Capability{
			{Name: methods.RunExecute, Version: 1},
			{Name: methods.RunCancel, Version: 1},
			{Name: methods.WorkerList, Version: 1},
			{Name: methods.WorkerAssignmentCancel, Version: 1},
			{Name: methods.WorkerMessageSend, Version: 1},
			{Name: methods.WorkerMessageReceive, Version: 1},
			{Name: methods.WorkerPoolStatus, Version: 1},
		},
	})
	if err != nil {
		return err
	}
	var result methods.InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode runtime initialize result: %w", err)
	}
	if result.ProtocolVersion != events.ProtocolVersionV2 {
		return fmt.Errorf("runtime protocol version %q is incompatible with %q", result.ProtocolVersion, events.ProtocolVersionV2)
	}
	return nil
}

func (c *Client) Execute(ctx context.Context, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	return c.ExecuteWithMode(ctx, "single_core", params)
}

func (c *Client) ExecuteWithMode(ctx context.Context, mode string, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	if normalizedRuntimeMode(mode) == "per_run_process" {
		return c.executePerRun(ctx, params)
	}
	// For the shared core process, try to influence pool size before first start.
	c.setWorkerPoolSize(params.Options.WorkerPoolSize)
	if err := c.Initialize(ctx); err != nil {
		return methods.RunExecuteResult{}, err
	}
	raw, err := c.call(ctx, methods.RunExecute, params)
	if err != nil {
		return methods.RunExecuteResult{}, err
	}
	var accepted methods.RunExecuteResult
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return methods.RunExecuteResult{}, err
	}
	return accepted, nil
}

func (c *Client) CancelRun(ctx context.Context, params methods.RunCancelParams) (methods.RunCancelResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.CancelRun(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.RunCancelResult{}, err
	}
	raw, err := c.call(ctx, methods.RunCancel, params)
	if err != nil {
		return methods.RunCancelResult{}, err
	}
	var result methods.RunCancelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.RunCancelResult{}, err
	}
	return result, nil
}

func (c *Client) Workers(ctx context.Context, params methods.WorkerListParams) (methods.WorkerListResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.Workers(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerListResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerList, params)
	if err != nil {
		return methods.WorkerListResult{}, err
	}
	var result methods.WorkerListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerListResult{}, err
	}
	return result, nil
}

func (c *Client) CancelAssignment(ctx context.Context, params methods.WorkerAssignmentCancelParams) (methods.WorkerAssignmentCancelResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.CancelAssignment(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerAssignmentCancel, params)
	if err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	var result methods.WorkerAssignmentCancelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerAssignmentCancelResult{}, err
	}
	return result, nil
}

func (c *Client) SendWorkerMessage(ctx context.Context, params methods.WorkerMessageSendParams) (methods.WorkerMessageSendResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerMessageSend, params)
	if err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	var result methods.WorkerMessageSendResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerMessageSendResult{}, err
	}
	return result, nil
}

func (c *Client) ReceiveWorkerMessage(ctx context.Context, params methods.WorkerMessageReceiveParams) (methods.WorkerMessageReceiveResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerMessageReceive, params)
	if err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	var result methods.WorkerMessageReceiveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerMessageReceiveResult{}, err
	}
	return result, nil
}

func (c *Client) WorkerPoolStatus(ctx context.Context) (methods.WorkerPoolStatusResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	raw, err := c.call(ctx, methods.WorkerPoolStatus, methods.WorkerPoolStatusParams{})
	if err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	var result methods.WorkerPoolStatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.WorkerPoolStatusResult{}, err
	}
	return result, nil
}

func (c *Client) ResolvePermission(ctx context.Context, params permission.ResolveParams) (permission.ResolveResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.ResolvePermission(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return permission.ResolveResult{}, err
	}
	raw, err := c.call(ctx, methods.PermissionResolve, params)
	if err != nil {
		return permission.ResolveResult{}, err
	}
	var result permission.ResolveResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return permission.ResolveResult{}, err
	}
	return result, nil
}

func (c *Client) DiscoverMCP(ctx context.Context, params methods.MCPDiscoverParams) (protocolmcp.MCPDiscoveryResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	raw, err := c.call(ctx, methods.MCPDiscover, params)
	if err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	var result protocolmcp.MCPDiscoveryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return protocolmcp.MCPDiscoveryResult{}, err
	}
	return result, nil
}

func (c *Client) ListSkills(ctx context.Context, params methods.SkillsListParams) (methods.SkillsListResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillsListResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkills, params)
	if err != nil {
		return methods.SkillsListResult{}, err
	}
	var result methods.SkillsListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillsListResult{}, err
	}
	return result, nil
}

func (c *Client) LoadSkill(ctx context.Context, params methods.SkillLoadParams) (methods.SkillLoadResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillLoadResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillLoad, params)
	if err != nil {
		return methods.SkillLoadResult{}, err
	}
	var result methods.SkillLoadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillLoadResult{}, err
	}
	return result, nil
}

func (c *Client) CreateSkill(ctx context.Context, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillMutateResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillCreate, params)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	var result methods.SkillMutateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillMutateResult{}, err
	}
	return result, nil
}

func (c *Client) UpdateSkill(ctx context.Context, params methods.SkillMutateParams) (methods.SkillMutateResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillMutateResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillUpdate, params)
	if err != nil {
		return methods.SkillMutateResult{}, err
	}
	var result methods.SkillMutateResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillMutateResult{}, err
	}
	return result, nil
}

func (c *Client) DeleteSkill(ctx context.Context, params methods.SkillDeleteParams) (methods.SkillDeleteResult, error) {
	if err := c.Initialize(ctx); err != nil {
		return methods.SkillDeleteResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSkillDelete, params)
	if err != nil {
		return methods.SkillDeleteResult{}, err
	}
	var result methods.SkillDeleteResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SkillDeleteResult{}, err
	}
	return result, nil
}

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

func (c *Client) executePerRun(ctx context.Context, params methods.RunExecuteParams) (methods.RunExecuteResult, error) {
	if params.RunID == "" {
		return methods.RunExecuteResult{}, fmt.Errorf("run_id is required")
	}
	var child *Client
	child = New(c.command, c.args, c.version, func(event events.EnvelopeV2) {
		if c.onEvent != nil {
			c.onEvent(event)
		}
		// Release dedicated process on terminal events so multi-session slots free promptly.
		if event.RunID == params.RunID && (event.Type == events.EventFinish || event.Type == events.EventError) {
			// Detach synchronously so the process-exit callback cannot race a valid
			// terminal event and incorrectly turn a completed run into a failure.
			if c.removePerRun(params.RunID, child) {
				go func() { _ = child.Shutdown(context.Background()) }()
			}
		}
	}, c.onRequest)
	child.onExit = func(err error) {
		if !c.removePerRun(params.RunID, child) {
			return
		}
		c.mu.Lock()
		handler := c.onRunExit
		c.mu.Unlock()
		if handler != nil {
			handler(params.RunID, err)
		}
	}
	// Propagate worker pool size (and any future extra env) to the dedicated child.
	child.setWorkerPoolSize(params.Options.WorkerPoolSize)
	c.mu.Lock()
	if _, exists := c.perRuns[params.RunID]; exists {
		c.mu.Unlock()
		return methods.RunExecuteResult{}, fmt.Errorf("per-run runtime already exists for %s", params.RunID)
	}
	c.perRuns[params.RunID] = child
	c.mu.Unlock()

	accepted, err := child.Execute(ctx, params)
	if err != nil {
		c.removePerRun(params.RunID, child)
		_ = child.Shutdown(context.Background())
		return methods.RunExecuteResult{}, err
	}
	return accepted, nil
}

func (c *Client) perRun(runID string) *Client {
	if runID == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.perRuns[runID]
}

func (c *Client) removePerRun(runID string, child *Client) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perRuns[runID] != child {
		return false
	}
	delete(c.perRuns, runID)
	return true
}

func (c *Client) releasePerRun(runID string, child *Client) {
	if c.removePerRun(runID, child) {
		_ = child.Shutdown(context.Background())
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

// runtimeTransport selects the gateway↔agent wire transport.
// Default is IPC (Windows named pipes / Unix domain sockets).
// Set RED_PANDA_RUNTIME_IPC=0 (or "false"/"stdio") to force legacy stdio.
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
