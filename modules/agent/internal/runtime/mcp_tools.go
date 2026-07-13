package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// mcpToolBinding maps a provider-facing canonical name to a server + raw tool.
type mcpToolBinding struct {
	Config  mcp.MCPServerConfig
	RawName string
	Def     tools.Definition
}

// prepareMCPToolsForRun discovers tools from enabled MCP server configs on the
// reply options and registers bindings for tools/call (docs/36 D2 / docs/19).
// Discovery failures for one server omit that server's tools; Runtime stays up.
func (r *Runtime) prepareMCPToolsForRun(ctx context.Context, params methods.ReplyParams) []tools.Definition {
	servers := params.Options.MCPServers
	if len(servers) == 0 {
		return nil
	}
	workspace := params.Session.WorkingDir
	bindings := make(map[string]mcpToolBinding)
	defs := make([]tools.Definition, 0)
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		if strings.TrimSpace(server.Command) == "" || strings.TrimSpace(server.Name) == "" {
			continue
		}
		discovery := r.discoverMCPServer(ctx, workspace, server)
		if discovery.Status != "ready" {
			fmt.Fprintf(r.log, "mcp discover %s: %s (%s)\n", server.Name, discovery.Status, discovery.Error)
			continue
		}
		for _, tool := range discovery.Tools {
			canonical := mcpCanonicalName(server.Name, tool.Name)
			if canonical == "" {
				continue
			}
			if _, exists := bindings[canonical]; exists {
				// Prefer the first registered server/tool pair; skip duplicates.
				continue
			}
			def := tools.Definition{
				Name:        canonical,
				DisplayName: "MCP " + server.Name + " / " + tool.Name,
				Description: strings.TrimSpace(tool.Description),
				Risk:        mcpToolRisk(server, tool.Name),
				Parameters:  mcpParametersSchema(tool.InputSchema),
			}
			if def.Description == "" {
				def.Description = "MCP tool " + tool.Name + " on server " + server.Name
			}
			bindings[canonical] = mcpToolBinding{Config: server, RawName: tool.Name, Def: def}
			defs = append(defs, def)
		}
	}
	r.setMCPBindings(params.RunID, bindings)
	return defs
}

func (r *Runtime) setMCPBindings(runID string, bindings map[string]mcpToolBinding) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mcpBindings == nil {
		r.mcpBindings = map[string]map[string]mcpToolBinding{}
	}
	if len(bindings) == 0 {
		delete(r.mcpBindings, runID)
		return
	}
	r.mcpBindings[runID] = bindings
}

func (r *Runtime) clearMCPBindings(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mcpBindings, runID)
}

func (r *Runtime) mcpBinding(runID, canonical string) (mcpToolBinding, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byRun := r.mcpBindings[runID]
	if byRun == nil {
		return mcpToolBinding{}, false
	}
	b, ok := byRun[canonical]
	return b, ok
}

func (r *Runtime) mcpDefinitionsForRun(runID string) []tools.Definition {
	r.mu.Lock()
	defer r.mu.Unlock()
	byRun := r.mcpBindings[runID]
	if len(byRun) == 0 {
		return nil
	}
	out := make([]tools.Definition, 0, len(byRun))
	for _, b := range byRun {
		out = append(out, b.Def)
	}
	return out
}

func (r *Runtime) toolsForReply(ctx context.Context, params methods.ReplyParams) []tools.Definition {
	// Ensure bindings exist for this run (idempotent re-prepare is cheap enough for MVP;
	// discovery is the cost — only prepare once when empty).
	if len(params.Options.MCPServers) > 0 && len(r.mcpDefinitionsForRun(params.RunID)) == 0 {
		_ = r.prepareMCPToolsForRun(ctx, params)
	}
	base := r.tools.AvailableTools()
	mcpDefs := r.mcpDefinitionsForRun(params.RunID)
	if len(mcpDefs) == 0 {
		return base
	}
	out := make([]tools.Definition, 0, len(base)+len(mcpDefs))
	out = append(out, base...)
	out = append(out, mcpDefs...)
	return out
}

func (r *Runtime) executeMCPTool(ctx context.Context, runCtx ToolRunContext, call tools.Call) (string, error) {
	binding, ok := r.mcpBinding(runCtx.RunID, call.Name)
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %s", call.Name)
	}
	return r.callMCPTool(ctx, runCtx.WorkingDir, binding.Config, binding.RawName, call.Arguments)
}

// callMCPTool starts a one-shot MCP stdio session, runs tools/call, then cleans up.
// Process reuse is deferred to D3.
func (r *Runtime) callMCPTool(
	ctx context.Context,
	workspaceRoot string,
	config mcp.MCPServerConfig,
	rawToolName string,
	arguments map[string]any,
) (output string, err error) {
	if !config.Enabled {
		return "", fmt.Errorf("MCP server %s is disabled", config.Name)
	}
	if strings.TrimSpace(config.Command) == "" {
		return "", fmt.Errorf("MCP server %s has empty command", config.Name)
	}
	if strings.TrimSpace(rawToolName) == "" {
		return "", fmt.Errorf("MCP tool name is required")
	}
	timeouts := config.Timeouts.Normalized()
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Env = append(os.Environ(), envPairs(config.Env)...)
	if config.CWD != "" {
		cmd.Dir = config.CWD
		if !filepath.IsAbs(cmd.Dir) && workspaceRoot != "" {
			cmd.Dir = filepath.Join(workspaceRoot, config.CWD)
		}
	} else if workspaceRoot != "" {
		cmd.Dir = workspaceRoot
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("start failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return "", fmt.Errorf("start failed: %w", err)
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
	r.registerMCPProcess(process)
	defer func() {
		process.close(durationMillis(timeouts.ShutdownMS))
		r.unregisterMCPProcess(process)
		if err != nil {
			msg := redactMCPSecrets(err.Error(), config.Env)
			if summary := stderr.String(); summary != "" {
				msg = msg + " (stderr: " + redactMCPSecrets(summary, config.Env) + ")"
			}
			err = fmt.Errorf("%s", msg)
		}
	}()

	startResult := make(chan error, 1)
	go func() { startResult <- process.start() }()
	select {
	case err = <-startResult:
		if err != nil {
			return "", fmt.Errorf("start failed: %w", err)
		}
	case <-time.After(durationMillis(timeouts.StartMS)):
		return "", fmt.Errorf("start timeout after %dms", timeouts.StartMS)
	case <-ctx.Done():
		return "", fmt.Errorf("start failed: %w", ctx.Err())
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
			"clientInfo":      map[string]any{"name": "red-panda-agent", "version": r.version},
		},
	}
	if err = writeMCPMessage(stdin, initialize); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}
	var initResult map[string]any
	if err = waitMCPResponse(ctx, lines, readErrors, process, 1, durationMillis(timeouts.InitializeMS), &initResult); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}
	if err = writeMCPMessage(stdin, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return "", fmt.Errorf("initialize failed: %w", err)
	}

	if arguments == nil {
		arguments = map[string]any{}
	}
	callReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      rawToolName,
			"arguments": arguments,
		},
	}
	if err = writeMCPMessage(stdin, callReq); err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}
	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err = waitMCPResponse(ctx, lines, readErrors, process, 3, durationMillis(timeouts.CallMS), &callResult); err != nil {
		return "", fmt.Errorf("tools/call failed: %w", err)
	}
	var parts []string
	for _, block := range callResult.Content {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		parts = append(parts, block.Text)
	}
	text := strings.Join(parts, "\n")
	if callResult.IsError {
		if text == "" {
			text = "MCP tool returned isError"
		}
		return text, fmt.Errorf("%s", text)
	}
	if text == "" {
		// Preserve a structured empty success for Activity/UI.
		raw, _ := json.Marshal(callResult)
		text = string(raw)
	}
	if len(text) > maxToolOutputBytes {
		text = text[:maxToolOutputBytes] + "\n…(truncated)"
	}
	return text, nil
}

// mcpCanonicalName builds mcp__server__tool (design docs/19 §8.1).
func mcpCanonicalName(serverName, rawTool string) string {
	server := strings.TrimSpace(serverName)
	raw := strings.TrimSpace(rawTool)
	if server == "" || raw == "" {
		return ""
	}
	return "mcp__" + server + "__" + sanitizeMCPToken(raw)
}

func sanitizeMCPToken(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '.' || r == '/' || r == '\\' || r == ':':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	for strings.Contains(out, "__") {
		// Keep double-underscore only as the mcp name separator; collapse inside token.
		out = strings.ReplaceAll(out, "__", "_")
	}
	return strings.Trim(out, "_")
}

func mcpToolRisk(config mcp.MCPServerConfig, rawTool string) tools.Risk {
	if config.RiskOverrides != nil {
		if risk, ok := config.RiskOverrides[rawTool]; ok {
			switch strings.ToLower(strings.TrimSpace(risk)) {
			case "low":
				return tools.RiskLow
			case "medium":
				return tools.RiskMedium
			case "high":
				return tools.RiskHigh
			}
		}
	}
	// Default high until a trusted metadata contract exists (docs/19).
	return tools.RiskHigh
}

func mcpParametersSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	// Shallow clone so callers cannot mutate discovery cache.
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	return out
}

func isMCPToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}
