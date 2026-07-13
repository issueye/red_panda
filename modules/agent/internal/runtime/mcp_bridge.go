package runtime

import (
	"context"
	"encoding/json"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/jsonrpc"
	protomcp "redpanda/protocol/mcp"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// handleMCPDiscover proxies JSON-RPC mcp.discover to the MCP manager.
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

func (r *Runtime) toolsForReply(ctx context.Context, params methods.ReplyParams) []tools.Definition {
	base := r.tools.AvailableTools()
	if r.mcp == nil || len(params.Options.MCPServers) == 0 {
		return base
	}
	if len(r.mcp.DefinitionsForRun(params.RunID)) == 0 {
		_ = r.mcp.PrepareToolsForRun(ctx, params)
	}
	mcpDefs := r.mcp.DefinitionsForRun(params.RunID)
	if len(mcpDefs) == 0 {
		return base
	}
	out := make([]tools.Definition, 0, len(base)+len(mcpDefs))
	out = append(out, base...)
	out = append(out, mcpDefs...)
	return out
}

func (r *Runtime) mcpDefinitionsForRun(runID string) []tools.Definition {
	if r.mcp == nil {
		return nil
	}
	return r.mcp.DefinitionsForRun(runID)
}

func (r *Runtime) executeMCPTool(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	return r.mcp.ExecuteTool(ctx, runCtx.RunID, runCtx.WorkingDir, call)
}
