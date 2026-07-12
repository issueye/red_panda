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

type EventHandler func(events.Envelope)

type RequestHandler func(context.Context, string, json.RawMessage) (any, error)

const maxRuntimeJSONRPCLineBytes = 4 * 1024 * 1024

type Client struct {
	command   string
	args      []string
	version   string
	onEvent   EventHandler
	onRequest RequestHandler

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	ipcCloser io.Closer // session listener when transport is IPC
	pending   map[jsonrpc.ID]chan jsonrpc.Response
	nextID    uint64
	running   bool
	perRuns   map[string]*Client
	transport string // "stdio" or "ipc"
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
	_, err := c.call(ctx, methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersion,
		Client: methods.PeerInfo{
			Name:    "red-panda-gateway",
			Version: c.version,
		},
		Environment: methods.Environment{
			PermissionMode: "strict",
		},
		Capabilities: []methods.Capability{
			{Name: methods.AgentEvent, Version: 1},
		},
	})
	return err
}

func (c *Client) Reply(ctx context.Context, params methods.ReplyParams) (methods.ReplyAccepted, error) {
	return c.ReplyWithMode(ctx, "single_core", params)
}

func (c *Client) ReplyWithMode(ctx context.Context, mode string, params methods.ReplyParams) (methods.ReplyAccepted, error) {
	if normalizedRuntimeMode(mode) == "per_run_process" {
		return c.replyPerRun(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.ReplyAccepted{}, err
	}
	raw, err := c.call(ctx, methods.AgentReply, params)
	if err != nil {
		return methods.ReplyAccepted{}, err
	}
	var accepted methods.ReplyAccepted
	if err := json.Unmarshal(raw, &accepted); err != nil {
		return methods.ReplyAccepted{}, err
	}
	return accepted, nil
}

func (c *Client) Cancel(ctx context.Context, params methods.CancelParams) error {
	if child := c.perRun(params.RunID); child != nil {
		return child.Cancel(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return err
	}
	_, err := c.call(ctx, methods.AgentCancel, params)
	return err
}

func (c *Client) SubAgents(ctx context.Context, params methods.SubAgentsParams) (methods.SubAgentsResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.SubAgents(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.SubAgentsResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSubAgents, params)
	if err != nil {
		return methods.SubAgentsResult{}, err
	}
	var result methods.SubAgentsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SubAgentsResult{}, err
	}
	return result, nil
}

func (c *Client) CancelSubAgent(ctx context.Context, params methods.SubAgentCancelParams) (methods.SubAgentCancelResult, error) {
	if child := c.perRun(params.RunID); child != nil {
		return child.CancelSubAgent(ctx, params)
	}
	if err := c.Initialize(ctx); err != nil {
		return methods.SubAgentCancelResult{}, err
	}
	raw, err := c.call(ctx, methods.AgentSubAgentCancel, params)
	if err != nil {
		return methods.SubAgentCancelResult{}, err
	}
	var result methods.SubAgentCancelResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return methods.SubAgentCancelResult{}, err
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

func (c *Client) replyPerRun(ctx context.Context, params methods.ReplyParams) (methods.ReplyAccepted, error) {
	if params.RunID == "" {
		return methods.ReplyAccepted{}, fmt.Errorf("run_id is required")
	}
	var child *Client
	child = New(c.command, c.args, c.version, func(event events.Envelope) {
		if c.onEvent != nil {
			c.onEvent(event)
		}
		// Release dedicated process on terminal events so multi-session slots free promptly.
		if event.RootRunID == params.RunID && (event.Type == events.EventFinish || event.Type == events.EventError) {
			go c.releasePerRun(params.RunID, child)
		}
	}, c.onRequest)
	c.mu.Lock()
	if _, exists := c.perRuns[params.RunID]; exists {
		c.mu.Unlock()
		return methods.ReplyAccepted{}, fmt.Errorf("per-run runtime already exists for %s", params.RunID)
	}
	c.perRuns[params.RunID] = child
	c.mu.Unlock()

	accepted, err := child.Reply(ctx, params)
	if err != nil {
		c.removePerRun(params.RunID, child)
		_ = child.Shutdown(context.Background())
		return methods.ReplyAccepted{}, err
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

	proc, err := ipc.StartProcess(ctx, ipc.ProcessConfig{
		Command: c.command,
		Args:    c.args,
		Prefix:  "red-panda-agent",
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
	if method != methods.AgentEvent || c.onEvent == nil {
		return
	}
	var note jsonrpc.Notification
	if err := json.Unmarshal(line, &note); err != nil {
		return
	}
	var event events.Envelope
	if err := json.Unmarshal(note.Params, &event); err != nil {
		return
	}
	c.onEvent(event)
}

func (c *Client) drainStderr(stderr io.Reader) {
	_, _ = io.Copy(io.Discard, stderr)
}

func (c *Client) wait(cmd *exec.Cmd) {
	_ = cmd.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
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
	}
}
