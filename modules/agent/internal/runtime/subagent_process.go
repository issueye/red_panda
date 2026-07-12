package runtime

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
	"redpanda/protocol/methods"
)

type subAgentProcess struct {
	command   string
	args      []string
	version   string
	log       io.Writer
	onRequest func(context.Context, string, any) (json.RawMessage, error)

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	ipcCloser io.Closer
	pending   map[jsonrpc.ID]chan jsonrpc.Response
	nextID    uint64
	events    chan events.Envelope
	done      chan struct{}
	running   bool
}

func newSubAgentProcess(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
	return newSubAgentProcessWithRequestHandler(ctx, params, subAgentID, nil)
}

func newSubAgentProcessWithRequestHandler(
	ctx context.Context,
	params methods.ReplyParams,
	subAgentID string,
	onRequest func(context.Context, string, any) (json.RawMessage, error),
) (processSubAgent, error) {
	command, args, err := subAgentCommand()
	if err != nil {
		return nil, err
	}
	child := &subAgentProcess{
		command:   command,
		args:      args,
		version:   "dev",
		log:       io.Discard,
		onRequest: onRequest,
		pending:   map[jsonrpc.ID]chan jsonrpc.Response{},
		events:    make(chan events.Envelope, 256),
		done:      make(chan struct{}),
	}
	if err := child.start(ctx); err != nil {
		return nil, err
	}
	if _, err := child.call(ctx, methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersion,
		Client:          methods.PeerInfo{Name: "red-panda-agent-parent", Version: child.version},
		WorkspaceRoot:   params.Session.WorkingDir,
		Environment: methods.Environment{
			PermissionMode: params.Options.PermissionMode,
		},
		Capabilities: []methods.Capability{
			{Name: methods.AgentEvent, Version: 1},
		},
	}); err != nil {
		_ = child.Close(context.Background())
		return nil, err
	}
	return child, nil
}

func subAgentCommand() (string, []string, error) {
	override := strings.TrimSpace(os.Getenv("RED_PANDA_SUBAGENT_COMMAND"))
	if override != "" {
		parts := strings.Fields(override)
		if len(parts) == 0 {
			return "", nil, fmt.Errorf("RED_PANDA_SUBAGENT_COMMAND is empty")
		}
		return parts[0], parts[1:], nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return exe, nil, nil
}

func (c *subAgentProcess) Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error {
	if _, err := c.call(ctx, methods.AgentReply, childParams); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_ = c.Cancel(context.Background(), childParams.RunID, "parent cancelled")
			return ctx.Err()
		case <-c.done:
			return errors.New("subagent runtime process stopped")
		case event := <-c.events:
			if onEvent != nil {
				onEvent(event)
			}
			if event.Type == events.EventFinish {
				status, _ := event.Payload["status"].(string)
				if status == "cancelled" {
					return context.Canceled
				}
				return nil
			}
		}
	}
}

func (c *subAgentProcess) Cancel(ctx context.Context, runID string, reason string) error {
	_, err := c.call(ctx, methods.AgentCancel, methods.CancelParams{RunID: runID, Reason: reason})
	return err
}

func (c *subAgentProcess) Close(ctx context.Context) error {
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

// subAgentTransport mirrors gateway default: IPC unless explicitly forced to stdio.
func subAgentTransport() string {
	v := strings.TrimSpace(os.Getenv("RED_PANDA_RUNTIME_IPC"))
	switch strings.ToLower(v) {
	case "0", "false", "no", "stdio", "off":
		return "stdio"
	default:
		return "ipc"
	}
}

func (c *subAgentProcess) start(ctx context.Context) error {
	if subAgentTransport() == "ipc" {
		return c.startIPC(ctx)
	}
	return c.startStdio(ctx)
}

func (c *subAgentProcess) startStdio(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := exec.CommandContext(ctx, c.command, c.args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.running = true
	go c.readStdout(stdout)
	go func() { _, _ = io.Copy(c.log, stderr) }()
	go c.wait(cmd)
	return nil
}

func (c *subAgentProcess) startIPC(ctx context.Context) error {
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	proc, err := ipc.StartProcess(ctx, ipc.ProcessConfig{
		Command: c.command,
		Args:    c.args,
		Prefix:  "red-panda-subagent",
		Configure: func(cmd *exec.Cmd) {
			cmd.Stderr = stderrWriter
		},
	})
	if err != nil {
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return err
	}
	_ = stderrWriter.Close()

	c.mu.Lock()
	c.cmd = proc.Cmd
	c.stdin = proc.Conn
	c.ipcCloser = proc.Session
	c.running = true
	c.mu.Unlock()

	go c.readStdout(proc.Conn)
	go func() { _, _ = io.Copy(c.log, stderrReader) }()
	go c.wait(proc.Cmd)
	return nil
}

func (c *subAgentProcess) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
		return nil, errors.New("subagent runtime process is not running")
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
		return nil, fmt.Errorf("subagent runtime request %s timed out", method)
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("subagent runtime process stopped")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("subagent runtime error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *subAgentProcess) nextRequestID() jsonrpc.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return jsonrpc.ID(fmt.Sprintf("sub_%d", c.nextID))
}

func (c *subAgentProcess) removePending(id jsonrpc.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *subAgentProcess) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxJSONRPCLineBytes)
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
			requestLine := append([]byte(nil), line...)
			go c.handleRequest(requestLine)
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

func (c *subAgentProcess) handleRequest(line []byte) {
	var req jsonrpc.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	if c.onRequest == nil {
		_ = c.writeResponse(jsonrpc.NewError(req.ID, -32601, "method not found"))
		return
	}

	// Return a proxy error before the child runtime reaches its own 30-second
	// request deadline, so a Gateway failure is explicit instead of ambiguous.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := c.onRequest(ctx, req.Method, req.Params)
	if err != nil {
		_ = c.writeResponse(jsonrpc.NewError(req.ID, -32050, err.Error()))
		return
	}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		_ = c.writeResponse(jsonrpc.NewError(req.ID, -32603, "internal error"))
		return
	}
	_ = c.writeResponse(resp)
}

func (c *subAgentProcess) writeResponse(resp jsonrpc.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return errors.New("subagent runtime process is not running")
	}
	_, err = fmt.Fprintln(c.stdin, string(raw))
	return err
}

func (c *subAgentProcess) handleNotification(line []byte, method string) {
	if method != methods.AgentEvent {
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
	c.enqueueEvent(event)
}

func (c *subAgentProcess) enqueueEvent(event events.Envelope) {
	if isDroppableSubAgentEvent(event.Type) {
		select {
		case c.events <- event:
		default:
		}
		return
	}

	// Lifecycle and final-message events must never disappear. Losing a
	// tool_finished/tool_failed event leaves the parent UI permanently running.
	select {
	case c.events <- event:
	case <-c.done:
	}
}

func isDroppableSubAgentEvent(typ events.EventType) bool {
	switch typ {
	case events.EventReasoningDelta, events.EventUsage, events.EventToolOutput:
		return true
	default:
		return false
	}
}

func (c *subAgentProcess) wait(cmd *exec.Cmd) {
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
		close(c.done)
	}
}
