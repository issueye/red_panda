// Package mcpkit wraps github.com/mark3labs/mcp-go for red_panda MCP stdio
// client and server use. Agent Runtime owns process lifecycle; Gateway only
// stores config DTOs (see docs/19-mcp-stdio-tools-design.md).
package mcpkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// MethodNotificationCancelled is the MCP protocol cancellation notification.
// Spec: either side may send this for an in-flight request previously issued
// in the same direction (docs/19 §7.8).
const MethodNotificationCancelled = "notifications/cancelled"

const (
	// DefaultProtocolVersion matches widely-deployed stdio servers.
	DefaultProtocolVersion = "2024-11-05"
	DefaultClientName      = "red-panda-agent"
	DefaultClientVersion   = "0.0.0"
	defaultStderrLimit     = 8192
)

// SessionConfig describes how to launch an MCP stdio server process.
type SessionConfig struct {
	Command       string
	Args          []string
	Env           map[string]string
	Dir           string // working directory; empty uses process default
	WorkspaceRoot string // resolves relative Dir
	ClientName    string
	ClientVersion string
	// ProtocolVersion requested during initialize; empty uses DefaultProtocolVersion.
	ProtocolVersion string
	StderrLimit     int
}

// Session is an MCP stdio client session (initialize + tools/* + close).
// After Initialize, tools/list and tools/call use the transport layer with a
// Session-owned request id so cancellations can emit notifications/cancelled.
type Session struct {
	client *client.Client
	cfg    SessionConfig
	stderr *boundedBuffer

	requestSeq atomic.Int64

	mu     sync.Mutex
	closed bool
}

// Open starts the MCP subprocess transport and returns an uninitialized Session.
// Caller must Initialize (or use Discover/Call helpers) and Close.
func Open(ctx context.Context, cfg SessionConfig) (*Session, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("command is required")
	}
	if cfg.ClientName == "" {
		cfg.ClientName = DefaultClientName
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = DefaultClientVersion
	}
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = DefaultProtocolVersion
	}
	if cfg.StderrLimit <= 0 {
		cfg.StderrLimit = defaultStderrLimit
	}

	dir := resolveDir(cfg.Dir, cfg.WorkspaceRoot)
	env := mergeEnv(cfg.Env)

	// Process lifetime must outlive the Open/start timeout context. CommandContext
	// would kill the child when the start deadline cancels, which breaks session
	// reuse (docs/19). Close() remains the authority for process teardown.
	stdioTransport := transport.NewStdioWithOptions(
		cfg.Command,
		env,
		cfg.Args,
		transport.WithCommandFunc(func(_ context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
			cmd := exec.Command(command, args...)
			cmd.Env = env
			if dir != "" {
				cmd.Dir = dir
			}
			return cmd, nil
		}),
	)

	c := client.NewClient(stdioTransport)
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("start failed: %w", err)
	}

	s := &Session{
		client: c,
		cfg:    cfg,
		stderr: &boundedBuffer{max: cfg.StderrLimit},
	}
	if r, ok := client.GetStderr(c); ok && r != nil {
		go s.drainStderr(r)
	}
	return s, nil
}

// Initialize performs MCP initialize + notifications/initialized.
func (s *Session) Initialize(ctx context.Context) (mcpsdk.Implementation, error) {
	req := mcpsdk.InitializeRequest{}
	req.Params.ProtocolVersion = s.cfg.ProtocolVersion
	req.Params.ClientInfo = mcpsdk.Implementation{
		Name:    s.cfg.ClientName,
		Version: s.cfg.ClientVersion,
	}
	req.Params.Capabilities = mcpsdk.ClientCapabilities{}

	result, err := s.client.Initialize(ctx, req)
	if err != nil {
		return mcpsdk.Implementation{}, err
	}
	// Client.Initialize consumes request id 1; Session RPCs continue from 2+.
	s.requestSeq.Store(1)
	return result.ServerInfo, nil
}

// ListTools calls tools/list (first page) and returns tool definitions.
func (s *Session) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	raw, err := s.sendRPC(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []mcpsdk.Tool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	out := make([]ToolDefinition, 0, len(parsed.Tools))
	for _, tool := range parsed.Tools {
		out = append(out, toolDefinitionFromSDK(tool))
	}
	return out, nil
}

// CallTool invokes tools/call and returns concatenated text content.
// When ctx is cancelled (run cancel or call timeout), Runtime sends
// notifications/cancelled with the in-flight request id (docs/19 §7.8) and
// still unblocks via context cancellation even if the server ignores it.
func (s *Session) CallTool(ctx context.Context, name string, arguments map[string]any) (text string, isError bool, err error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	raw, err := s.sendRPC(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": arguments,
	})
	if err != nil {
		return "", false, err
	}
	result, err := mcpsdk.ParseCallToolResult(&raw)
	if err != nil {
		return "", false, err
	}
	var parts []string
	for _, block := range result.Content {
		t := strings.TrimSpace(mcpsdk.GetTextFromContent(block))
		if t != "" {
			parts = append(parts, t)
		}
	}
	text = strings.Join(parts, "\n")
	return text, result.IsError, nil
}

// sendRPC issues a JSON-RPC request on the underlying transport with a
// Session-owned id. On ctx cancellation it best-effort emits
// notifications/cancelled so MCP servers can stop work (protocol-level cancel).
func (s *Session) sendRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("session closed")
	}
	if s.Closed() {
		return nil, fmt.Errorf("session closed")
	}
	tr := s.client.GetTransport()
	if tr == nil {
		return nil, fmt.Errorf("transport unavailable")
	}

	// Prefer ids >= 2 after Initialize (which uses id 1 via the mcp-go client).
	idValue := s.requestSeq.Add(1)
	if idValue < 2 {
		s.requestSeq.Store(1)
		idValue = s.requestSeq.Add(1)
	}
	reqID := mcpsdk.NewRequestId(idValue)

	// Detach cancel notification from the call ctx so we can still write after cancel.
	notifyCtx := context.WithoutCancel(ctx)
	callCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	var cancelOnce sync.Once
	go func() {
		select {
		case <-ctx.Done():
			cancelOnce.Do(func() {
				reason := "context canceled"
				if ctx.Err() != nil {
					reason = ctx.Err().Error()
				}
				_ = s.NotifyCancelled(notifyCtx, reqID, reason)
				cancelWatch()
			})
		case <-callCtx.Done():
		}
	}()

	resp, err := tr.SendRequest(callCtx, transport.JSONRPCRequest{
		JSONRPC: mcpsdk.JSONRPC_VERSION,
		ID:      reqID,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		// Ensure cancel was attempted when the caller cancelled/timed out.
		if ctx.Err() != nil {
			cancelOnce.Do(func() {
				reason := ctx.Err().Error()
				_ = s.NotifyCancelled(notifyCtx, reqID, reason)
			})
		}
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty response")
	}
	if resp.Error != nil {
		return nil, resp.Error.AsError()
	}
	if len(resp.Result) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return resp.Result, nil
}

// NotifyCancelled sends notifications/cancelled for a previously issued request id.
func (s *Session) NotifyCancelled(ctx context.Context, requestID mcpsdk.RequestId, reason string) error {
	if s == nil || s.client == nil || s.Closed() {
		return fmt.Errorf("session closed")
	}
	tr := s.client.GetTransport()
	if tr == nil {
		return fmt.Errorf("transport unavailable")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "cancelled"
	}
	notification := mcpsdk.JSONRPCNotification{
		JSONRPC: mcpsdk.JSONRPC_VERSION,
		Notification: mcpsdk.Notification{
			Method: MethodNotificationCancelled,
			Params: mcpsdk.NotificationParams{
				AdditionalFields: map[string]any{
					"requestId": requestID.Value(),
					"reason":    reason,
				},
			},
		},
	}
	return tr.SendNotification(ctx, notification)
}

// StderrSummary returns a bounded, already-captured stderr snapshot.
func (s *Session) StderrSummary() string {
	if s.stderr == nil {
		return ""
	}
	return s.stderr.String()
}

// Closed reports whether Close has already been called.
func (s *Session) Closed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Close shuts down the transport and child process.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// Underlying returns the raw mcp-go client for advanced use.
func (s *Session) Underlying() *client.Client {
	return s.client
}

func (s *Session) drainStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			_, _ = s.stderr.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// ToolDefinition is a transport-neutral tool schema used by Runtime / Gateway DTOs.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

func toolDefinitionFromSDK(tool mcpsdk.Tool) ToolDefinition {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if raw, err := json.Marshal(tool.InputSchema); err == nil {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err == nil && len(decoded) > 0 {
			schema = decoded
		}
	}
	return ToolDefinition{
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: schema,
	}
}

// DiscoverOptions controls phase timeouts for a one-shot discovery session.
type DiscoverOptions struct {
	StartMS      int
	InitializeMS int
	ListMS       int
	ShutdownMS   int
	// Tracker, when set, registers the live session so CloseAll can interrupt it.
	Tracker *Tracker
}

// Discovery is the outcome of a one-shot tools/list probe.
type Discovery struct {
	ServerInfo    mcpsdk.Implementation
	Tools         []ToolDefinition
	StderrSummary string
	DurationMS    int64
	Error         string
	Status        string // ready | failed | disabled
}

// Discover starts a session, initializes, lists tools, then closes.
// Named return so defer redaction and duration always apply.
func Discover(ctx context.Context, cfg SessionConfig, opts DiscoverOptions) (result Discovery) {
	startedAt := time.Now()
	result.Status = "failed"
	result.Tools = []ToolDefinition{}
	defer func() {
		result.DurationMS = time.Since(startedAt).Milliseconds()
		result.Error = RedactSecrets(result.Error, cfg.Env)
		result.StderrSummary = RedactSecrets(result.StderrSummary, cfg.Env)
	}()

	if strings.TrimSpace(cfg.Command) == "" {
		result.Error = "start failed: command is required"
		return result
	}

	startCtx := ctx
	var startCancel context.CancelFunc
	if opts.StartMS > 0 {
		startCtx, startCancel = context.WithTimeout(ctx, time.Duration(opts.StartMS)*time.Millisecond)
		defer startCancel()
	}

	session, err := Open(startCtx, cfg)
	if err != nil {
		result.Error = err.Error()
		if isTimeout(err) && opts.StartMS > 0 {
			result.Error = fmt.Sprintf("start timeout after %dms", opts.StartMS)
		}
		return result
	}
	if opts.Tracker != nil {
		opts.Tracker.Track(session)
	}
	defer func() {
		_ = session.Close()
		if opts.Tracker != nil {
			opts.Tracker.Untrack(session)
		}
		// Allow stderr drain to finish after the pipe closes.
		deadline := time.Now().Add(150 * time.Millisecond)
		for time.Now().Before(deadline) {
			if len(session.StderrSummary()) > 0 || session.closed {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		// Always snapshot after close; drain may complete on EOF.
		time.Sleep(20 * time.Millisecond)
		result.StderrSummary = session.StderrSummary()
	}()

	initCtx := ctx
	var initCancel context.CancelFunc
	if opts.InitializeMS > 0 {
		initCtx, initCancel = context.WithTimeout(ctx, time.Duration(opts.InitializeMS)*time.Millisecond)
		defer initCancel()
	}
	info, err := session.Initialize(initCtx)
	if err != nil {
		result.Error = "initialize failed: " + formatPhaseError(err)
		return result
	}
	result.ServerInfo = info

	listCtx := ctx
	var listCancel context.CancelFunc
	if opts.ListMS > 0 {
		listCtx, listCancel = context.WithTimeout(ctx, time.Duration(opts.ListMS)*time.Millisecond)
		defer listCancel()
	}
	tools, err := session.ListTools(listCtx)
	if err != nil {
		result.Error = "tools/list failed: " + formatPhaseError(err)
		return result
	}
	result.Tools = tools
	result.Status = "ready"
	return result
}

// CallOptions controls phase timeouts for a one-shot tools/call session.
type CallOptions struct {
	StartMS      int
	InitializeMS int
	CallMS       int
	ShutdownMS   int
	MaxOutput    int
	Tracker      *Tracker
}

// Call starts a session, initializes, calls one tool, then closes.
func Call(ctx context.Context, cfg SessionConfig, toolName string, arguments map[string]any, opts CallOptions) (output string, err error) {
	if strings.TrimSpace(toolName) == "" {
		return "", fmt.Errorf("MCP tool name is required")
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return "", fmt.Errorf("command is required")
	}

	startCtx := ctx
	var startCancel context.CancelFunc
	if opts.StartMS > 0 {
		startCtx, startCancel = context.WithTimeout(ctx, time.Duration(opts.StartMS)*time.Millisecond)
		defer startCancel()
	}

	session, openErr := Open(startCtx, cfg)
	if openErr != nil {
		msg := openErr.Error()
		if isTimeout(openErr) && opts.StartMS > 0 {
			msg = fmt.Sprintf("start timeout after %dms", opts.StartMS)
		}
		return "", fmt.Errorf("%s", msg)
	}
	if opts.Tracker != nil {
		opts.Tracker.Track(session)
	}
	defer func() {
		_ = session.Close()
		if opts.Tracker != nil {
			opts.Tracker.Untrack(session)
		}
		if err != nil {
			msg := RedactSecrets(err.Error(), cfg.Env)
			if summary := RedactSecrets(session.StderrSummary(), cfg.Env); summary != "" {
				msg = msg + " (stderr: " + summary + ")"
			}
			err = fmt.Errorf("%s", msg)
		}
	}()

	initCtx := ctx
	var initCancel context.CancelFunc
	if opts.InitializeMS > 0 {
		initCtx, initCancel = context.WithTimeout(ctx, time.Duration(opts.InitializeMS)*time.Millisecond)
		defer initCancel()
	}
	if _, initErr := session.Initialize(initCtx); initErr != nil {
		return "", fmt.Errorf("initialize failed: %s", formatPhaseError(initErr))
	}

	callCtx := ctx
	var callCancel context.CancelFunc
	if opts.CallMS > 0 {
		callCtx, callCancel = context.WithTimeout(ctx, time.Duration(opts.CallMS)*time.Millisecond)
		defer callCancel()
	}
	text, isErr, callErr := session.CallTool(callCtx, toolName, arguments)
	if callErr != nil {
		return "", fmt.Errorf("tools/call failed: %s", formatPhaseError(callErr))
	}
	if isErr {
		if text == "" {
			text = "MCP tool returned isError"
		}
		// Wrap with the same "tools/call failed:" prefix used by callErr above so
		// callers can classify this as a business-phase failure (vs startup-phase
		// spawn/initialize failures, which carry no such prefix). The text stays
		// intact as the user-visible tool output; only err.Error() gains a tag.
		return text, fmt.Errorf("tools/call failed: %s", text)
	}
	if text == "" {
		text = `{"content":[],"isError":false}`
	}
	if opts.MaxOutput > 0 && len(text) > opts.MaxOutput {
		text = text[:opts.MaxOutput] + "\n…(truncated)"
	}
	return text, nil
}

// Tracker tracks open sessions so Runtime can CloseAll on shutdown.
type Tracker struct {
	mu       sync.Mutex
	sessions map[*Session]struct{}
}

// NewTracker creates an empty session tracker.
func NewTracker() *Tracker {
	return &Tracker{sessions: map[*Session]struct{}{}}
}

// Track registers a session for later CloseAll.
func (t *Tracker) Track(s *Session) {
	if t == nil || s == nil {
		return
	}
	t.mu.Lock()
	t.sessions[s] = struct{}{}
	t.mu.Unlock()
}

// Untrack removes a session from the tracker.
func (t *Tracker) Untrack(s *Session) {
	if t == nil || s == nil {
		return
	}
	t.mu.Lock()
	delete(t.sessions, s)
	t.mu.Unlock()
}

// CloseAll closes every tracked session.
func (t *Tracker) CloseAll() {
	if t == nil {
		return
	}
	t.mu.Lock()
	list := make([]*Session, 0, len(t.sessions))
	for s := range t.sessions {
		list = append(list, s)
	}
	t.mu.Unlock()
	for _, s := range list {
		_ = s.Close()
		t.Untrack(s)
	}
}

// RedactSecrets replaces known secret values with **** (longest-first).
func RedactSecrets(value string, environment map[string]string) string {
	if value == "" || len(environment) == 0 {
		return value
	}
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

func resolveDir(dir, workspaceRoot string) string {
	if dir != "" {
		if filepath.IsAbs(dir) || workspaceRoot == "" {
			return dir
		}
		return filepath.Join(workspaceRoot, dir)
	}
	return workspaceRoot
}

func mergeEnv(extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context deadline") ||
		strings.Contains(msg, "context canceled") && strings.Contains(msg, "timeout")
}

func formatPhaseError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Preserve historical Runtime wording expected by tests / logs.
	if isTimeout(err) {
		return "timeout"
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "invalid character") ||
		strings.Contains(lower, "not-json") ||
		strings.Contains(lower, "cannot unmarshal") ||
		strings.Contains(lower, "looking for beginning") {
		return "invalid stdout JSON: " + msg
	}
	if strings.Contains(lower, "eof") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "file already closed") ||
		strings.Contains(lower, "transport closed") ||
		strings.Contains(lower, "process") {
		return "process exited: " + msg
	}
	return msg
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
