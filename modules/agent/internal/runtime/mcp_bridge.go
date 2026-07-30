package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/jsonrpc"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// handleMCPDiscover 将 JSON-RPC mcp.discover 代理给 MCP 管理器。
func (r *Runtime) handleMCPDiscover(ctx context.Context, req jsonrpc.Request) error {
	var params methods.MCPDiscoverParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	result := protomcp.MCPDiscoveryResult{Servers: make([]protomcp.MCPServerDiscovery, 0, len(params.Servers))}
	for _, server := range params.Servers {
		result.Servers = append(result.Servers, r.mcp.DiscoverServer(ctx, params.WorkspaceRoot, server))
	}
	resp, err := jsonrpc.NewResult(req.ID, result)
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

// handleMCPCall is the management-path tools/call for Settings try-call.
// It reuses Manager.CallTool (pooled session) without a root run binding.
func (r *Runtime) handleMCPCall(ctx context.Context, req jsonrpc.Request) error {
	var params methods.MCPCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	toolName := strings.TrimSpace(params.ToolName)
	if toolName == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "tool_name is required"))
	}
	if strings.TrimSpace(params.Server.Name) == "" || strings.TrimSpace(params.Server.Command) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "server name and command are required"))
	}
	if r.mcp == nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, "mcp manager unavailable"))
	}
	started := time.Now()
	output, err := r.mcp.CallTool(ctx, params.WorkspaceRoot, params.Server, toolName, params.Arguments)
	result := methods.MCPCallResult{
		Output:     output,
		OK:         err == nil,
		DurationMS: time.Since(started).Milliseconds(),
	}
	if err != nil {
		result.Error = err.Error()
		// When CallTool fails after producing tool text (isError), Output may still be set.
		if result.Output == "" {
			result.Output = err.Error()
		}
	}
	resp, writeErr := jsonrpc.NewResult(req.ID, result)
	if writeErr != nil {
		return writeErr
	}
	return r.writeResponse(resp)
}

func (r *Runtime) executeMCPTool(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	return r.mcp.ExecuteTool(ctx, runCtx.RunID, runCtx.WorkingDir, call)
}
