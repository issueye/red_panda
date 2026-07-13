package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	protomcp "redpanda/protocol/mcp"
)

const (
	mcpProtocolVersion = "2024-11-05"
	mcpStderrLimit     = 8192
)

type mcpProcess struct {
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	done            chan struct{}
	startDone       chan struct{}
	closeOne        sync.Once
	mu              sync.Mutex
	started         bool
	closing         bool
	waitErr         error
	shutdownTimeout time.Duration
}

type boundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.max - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buf.Write(p)
	}
	return n, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

func (m *Manager) DiscoverServer(ctx context.Context, workspaceRoot string, config protomcp.MCPServerConfig) (result protomcp.MCPServerDiscovery) {
	startedAt := time.Now()
	result.Name = config.Name
	result.Status = "failed"
	result.Tools = []protomcp.MCPToolDefinition{}
	defer func() { result.DurationMS = time.Since(startedAt).Milliseconds() }()
	defer func() {
		result.Error = redactMCPSecrets(result.Error, config.Env)
		result.StderrSummary = redactMCPSecrets(result.StderrSummary, config.Env)
	}()
	if !config.Enabled {
		result.Status = "disabled"
		return result
	}
	if strings.TrimSpace(config.Command) == "" {
		result.Error = "start failed: command is required"
		return result
	}

	timeouts := config.Timeouts.Normalized()
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Env = append(os.Environ(), envPairs(config.Env)...)
	if config.CWD != "" {
		cmd.Dir = config.CWD
		if !filepath.IsAbs(cmd.Dir) && workspaceRoot != "" {
			cmd.Dir = filepath.Join(workspaceRoot, cmd.Dir)
		}
	} else if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		result.Error = "start failed: " + err.Error()
		return result
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		result.Error = "start failed: " + err.Error()
		return result
	}
	stderr := &boundedBuffer{max: mcpStderrLimit}
	cmd.Stderr = stderr
	process := &mcpProcess{
		cmd:             cmd,
		stdin:           stdin,
		done:            make(chan struct{}),
		startDone:       make(chan struct{}),
		shutdownTimeout: durationMillis(timeouts.ShutdownMS),
	}
	m.registerProcess(process)
	defer func() {
		process.close(durationMillis(timeouts.ShutdownMS))
		m.unregisterProcess(process)
		result.StderrSummary = stderr.String()
	}()

	startResult := make(chan error, 1)
	go func() { startResult <- process.start() }()
	select {
	case err = <-startResult:
		if err != nil {
			result.Error = "start failed: " + err.Error()
			return result
		}
	case <-time.After(durationMillis(timeouts.StartMS)):
		result.Error = fmt.Sprintf("start timeout after %dms", timeouts.StartMS)
		return result
	case <-ctx.Done():
		result.Error = "start failed: " + ctx.Err().Error()
		return result
	}

	lines := make(chan []byte)
	readErrors := make(chan error, 1)
	stopReader := make(chan struct{})
	defer close(stopReader)
	go readMCPLines(stdout, lines, readErrors, stopReader)

	initialize := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "red-panda-agent", "version": m.version},
		},
	}
	if err = writeMCPMessage(stdin, initialize); err != nil {
		result.Error = "initialize failed: " + err.Error()
		return result
	}
	var initResult struct {
		ProtocolVersion string                 `json:"protocolVersion"`
		ServerInfo      protomcp.MCPServerInfo `json:"serverInfo"`
	}
	if err = waitMCPResponse(ctx, lines, readErrors, process, 1, durationMillis(timeouts.InitializeMS), &initResult); err != nil {
		result.Error = "initialize failed: " + err.Error()
		return result
	}
	result.ServerInfo = initResult.ServerInfo
	if err = writeMCPMessage(stdin, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		result.Error = "initialize failed: " + err.Error()
		return result
	}
	if err = writeMCPMessage(stdin, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}); err != nil {
		result.Error = "tools/list failed: " + err.Error()
		return result
	}
	var listResult struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err = waitMCPResponse(ctx, lines, readErrors, process, 2, durationMillis(timeouts.ListMS), &listResult); err != nil {
		result.Error = "tools/list failed: " + err.Error()
		return result
	}
	allowedTools := stringSet(config.ToolAllowlist)
	for _, tool := range listResult.Tools {
		if len(allowedTools) > 0 {
			if _, allowed := allowedTools[tool.Name]; !allowed {
				continue
			}
		}
		result.Tools = append(result.Tools, protomcp.MCPToolDefinition{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	result.Status = "ready"
	return result
}

func (p *mcpProcess) start() error {
	if err := p.cmd.Start(); err != nil {
		close(p.startDone)
		return err
	}
	p.mu.Lock()
	p.started = true
	closing := p.closing
	p.mu.Unlock()
	go func() {
		err := p.cmd.Wait()
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	}()
	close(p.startDone)
	if closing && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

func (p *mcpProcess) close(timeout time.Duration) {
	p.closeOne.Do(func() {
		_ = p.stdin.Close()
		p.mu.Lock()
		p.closing = true
		p.mu.Unlock()
		select {
		case <-p.startDone:
		case <-time.After(timeout):
			return
		}
		p.mu.Lock()
		started := p.started
		p.mu.Unlock()
		if !started {
			return
		}
		select {
		case <-p.done:
			return
		case <-time.After(timeout):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
			select {
			case <-p.done:
			case <-time.After(timeout):
			}
		}
	})
}

func (m *Manager) registerProcess(process *mcpProcess) {
	m.mu.Lock()
	m.processes[process] = struct{}{}
	m.mu.Unlock()
}

func (m *Manager) unregisterProcess(process *mcpProcess) {
	m.mu.Lock()
	delete(m.processes, process)
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	processes := make([]*mcpProcess, 0, len(m.processes))
	for process := range m.processes {
		processes = append(processes, process)
	}
	m.mu.Unlock()
	for _, process := range processes {
		process.close(process.shutdownTimeout)
	}
}

func readMCPLines(reader io.Reader, lines chan<- []byte, failures chan<- error, stopped <-chan struct{}) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if len(bytes.TrimSpace(line)) > 0 {
			select {
			case lines <- line:
			case <-stopped:
				return
			}
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	select {
	case failures <- err:
	case <-stopped:
	}
}

func waitMCPResponse(ctx context.Context, lines <-chan []byte, readErrors <-chan error, process *mcpProcess, id int, timeout time.Duration, target any) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line := <-lines:
			var response struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  json.RawMessage `json:"result"`
				Error   *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &response); err != nil {
				return fmt.Errorf("invalid stdout JSON: %w", err)
			}
			if len(response.ID) == 0 {
				continue
			}
			var responseID int
			if err := json.Unmarshal(response.ID, &responseID); err != nil || responseID != id {
				continue
			}
			if response.Error != nil {
				return fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
			}
			if len(response.Result) == 0 {
				return errors.New("response is missing result")
			}
			if err := json.Unmarshal(response.Result, target); err != nil {
				return fmt.Errorf("invalid result: %w", err)
			}
			return nil
		case err := <-readErrors:
			if errors.Is(err, io.EOF) {
				select {
				case <-process.done:
					process.mu.Lock()
					waitErr := process.waitErr
					process.mu.Unlock()
					if waitErr == nil {
						return errors.New("process exited")
					}
					return fmt.Errorf("process exited: %w", waitErr)
				case <-time.After(20 * time.Millisecond):
				}
			}
			return fmt.Errorf("stdout closed: %w", err)
		case <-process.done:
			process.mu.Lock()
			err := process.waitErr
			process.mu.Unlock()
			if err == nil {
				return errors.New("process exited")
			}
			return fmt.Errorf("process exited: %w", err)
		case <-timer.C:
			return fmt.Errorf("timeout after %s", timeout)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func writeMCPMessage(writer io.Writer, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = writer.Write(data)
	return err
}

func envPairs(values map[string]string) []string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

func redactMCPSecrets(value string, environment map[string]string) string {
	secrets := make([]string, 0, len(environment))
	for _, secret := range environment {
		if secret != "" {
			secrets = append(secrets, secret)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		value = strings.ReplaceAll(value, secret, "****")
	}
	return value
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func durationMillis(value int) time.Duration {
	return time.Duration(value) * time.Millisecond
}
