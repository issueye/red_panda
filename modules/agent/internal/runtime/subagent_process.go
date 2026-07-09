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

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

type subAgentProcess struct {
	command string
	args    []string
	version string
	log     io.Writer

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[jsonrpc.ID]chan jsonrpc.Response
	nextID  uint64
	events  chan events.Envelope
	done    chan struct{}
	running bool
}

func newSubAgentProcess(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
	command, args, err := subAgentCommand()
	if err != nil {
		return nil, err
	}
	child := &subAgentProcess{
		command: command,
		args:    args,
		version: "dev",
		log:     io.Discard,
		pending: map[jsonrpc.ID]chan jsonrpc.Response{},
		events:  make(chan events.Envelope, 64),
		done:    make(chan struct{}),
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
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.running = false
	return nil
}

func (c *subAgentProcess) start(ctx context.Context) error {
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
	for scanner.Scan() {
		line := scanner.Bytes()
		var probe struct {
			ID     jsonrpc.ID `json:"id"`
			Method string     `json:"method"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
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
	select {
	case c.events <- event:
	default:
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
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		close(c.done)
	}
}
