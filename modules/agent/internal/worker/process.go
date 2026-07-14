package worker

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

const maxJSONRPCLineBytes = 4 * 1024 * 1024

type runtimeProcess struct {
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
	events    chan events.EnvelopeV2
	done      chan struct{}
	running   bool
}

func NewProcess(ctx context.Context, params methods.ReplyParams, assignmentID string) (Process, error) {
	return NewProcessWithRequestHandler(ctx, params, assignmentID, nil)
}

func NewProcessWithRequestHandler(
	ctx context.Context,
	params methods.ReplyParams,
	assignmentID string,
	onRequest func(context.Context, string, any) (json.RawMessage, error),
) (Process, error) {
	command, args, err := workerCommand()
	if err != nil {
		return nil, err
	}
	child := &runtimeProcess{
		command:   command,
		args:      args,
		version:   "dev",
		log:       io.Discard,
		onRequest: onRequest,
		pending:   map[jsonrpc.ID]chan jsonrpc.Response{},
		events:    make(chan events.EnvelopeV2, 256),
		done:      make(chan struct{}),
	}
	if err := child.start(ctx); err != nil {
		return nil, err
	}
	if _, err := child.call(ctx, methods.CoreInitialize, methods.InitializeParams{
		ProtocolVersion: events.ProtocolVersionV2,
		Client:          methods.PeerInfo{Name: "red-panda-worker-host", Version: child.version},
		WorkspaceRoot:   params.Session.WorkingDir,
		Environment: methods.Environment{
			PermissionMode: params.Options.PermissionMode,
		},
		Capabilities: []methods.Capability{{Name: methods.RunEvent, Version: 1}},
	}); err != nil {
		_ = child.Close(context.Background())
		return nil, err
	}
	return child, nil
}

func workerCommand() (string, []string, error) {
	override := strings.TrimSpace(os.Getenv("RED_PANDA_WORKER_COMMAND"))
	if override != "" {
		parts := strings.Fields(override)
		if len(parts) == 0 {
			return "", nil, fmt.Errorf("RED_PANDA_WORKER_COMMAND is empty")
		}
		return parts[0], parts[1:], nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	return exe, nil, nil
}

func (c *runtimeProcess) Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.EnvelopeV2)) error {
	if _, err := c.call(ctx, methods.RunExecute, childParams); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_ = c.Cancel(context.Background(), childParams.RunID, "parent cancelled")
			return ctx.Err()
		case <-c.done:
			return errors.New("worker runtime process stopped")
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

func (c *runtimeProcess) Cancel(ctx context.Context, runID string, reason string) error {
	_, err := c.call(ctx, methods.RunCancel, methods.RunCancelParams{RunID: runID, Reason: reason})
	return err
}

// Healthy reports whether the reusable Runtime process can accept another run.
func (c *runtimeProcess) Healthy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running && c.stdin != nil
}

func (c *runtimeProcess) Close(ctx context.Context) error {
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

// workerTransport follows the Gateway transport default unless stdio is explicit.
func workerTransport() string {
	v := strings.TrimSpace(os.Getenv("RED_PANDA_RUNTIME_IPC"))
	switch strings.ToLower(v) {
	case "0", "false", "no", "stdio", "off":
		return "stdio"
	default:
		return "ipc"
	}
}

func (c *runtimeProcess) start(ctx context.Context) error {
	if workerTransport() == "ipc" {
		return c.startIPC(ctx)
	}
	return c.startStdio(ctx)
}

func (c *runtimeProcess) startStdio(ctx context.Context) error {
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

func (c *runtimeProcess) startIPC(ctx context.Context) error {
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	proc, err := ipc.StartProcess(ctx, ipc.ProcessConfig{
		Command: c.command,
		Args:    c.args,
		Prefix:  "red-panda-worker",
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

func (c *runtimeProcess) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
		return nil, errors.New("worker runtime process is not running")
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
		return nil, fmt.Errorf("worker runtime request %s timed out", method)
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("worker runtime process stopped")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("worker runtime error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *runtimeProcess) nextRequestID() jsonrpc.ID {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return jsonrpc.ID(fmt.Sprintf("worker_%d", c.nextID))
}

func (c *runtimeProcess) removePending(id jsonrpc.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *runtimeProcess) readStdout(stdout io.Reader) {
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

func (c *runtimeProcess) handleRequest(line []byte) {
	var req jsonrpc.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
	if c.onRequest == nil {
		_ = c.writeResponse(jsonrpc.NewError(req.ID, -32601, "method not found"))
		return
	}

	// 在子 Runtime 触发自身 30 秒请求期限前返回代理错误，
	// 使 Gateway 故障明确可见而非含糊不清。
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

func (c *runtimeProcess) writeResponse(resp jsonrpc.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running || c.stdin == nil {
		return errors.New("worker runtime process is not running")
	}
	_, err = fmt.Fprintln(c.stdin, string(raw))
	return err
}

func (c *runtimeProcess) handleNotification(line []byte, method string) {
	if method != methods.RunEvent {
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
	c.enqueueEvent(event)
}

func (c *runtimeProcess) enqueueEvent(event events.EnvelopeV2) {
	if isDroppableWorkerEvent(event.Type) {
		select {
		case c.events <- event:
		default:
		}
		return
	}

	// 生命周期和最终消息事件绝不能丢失。丢失 tool_finished 或 tool_failed 事件
	// 会使父 UI 永久显示为运行中。
	select {
	case c.events <- event:
	case <-c.done:
	}
}

func isDroppableWorkerEvent(typ events.EventType) bool {
	switch typ {
	case events.EventReasoningDelta, events.EventUsage, events.EventToolOutput:
		return true
	default:
		return false
	}
}

func (c *runtimeProcess) wait(cmd *exec.Cmd) {
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
