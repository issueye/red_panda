package mcpkit

import (
	"context"
	"fmt"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates an MCP server with tool capabilities and panic recovery.
func NewServer(name, version string, opts ...server.ServerOption) *server.MCPServer {
	base := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	}
	base = append(base, opts...)
	return server.NewMCPServer(name, version, base...)
}

// ToolHandler is the simplified tool callback used by red_panda MCP servers.
// Return isError=true for tool-level failures (still a successful JSON-RPC result).
type ToolHandler func(ctx context.Context, args map[string]any) (text string, isError bool)

// AddTextTool registers a tool that returns a single text content block.
func AddTextTool(s *server.MCPServer, tool mcpsdk.Tool, handler ToolHandler) {
	s.AddTool(tool, func(ctx context.Context, request mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		args := request.GetArguments()
		if args == nil {
			args = map[string]any{}
		}
		text, isErr := handler(ctx, args)
		if isErr {
			return mcpsdk.NewToolResultError(text), nil
		}
		return mcpsdk.NewToolResultText(text), nil
	})
}

// ServeStdio serves the MCP server over process stdin/stdout.
func ServeStdio(s *server.MCPServer) error {
	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp stdio server: %w", err)
	}
	return nil
}

// Re-export common mcp-go constructors so server binaries need only mcpkit.
var (
	NewTool         = mcpsdk.NewTool
	WithDescription = mcpsdk.WithDescription
	WithString      = mcpsdk.WithString
	WithNumber      = mcpsdk.WithNumber
	WithBoolean     = mcpsdk.WithBoolean
	WithObject      = mcpsdk.WithObject
	WithArray       = mcpsdk.WithArray
	Required        = mcpsdk.Required
	Description     = mcpsdk.Description
	NewToolResultText  = mcpsdk.NewToolResultText
	NewToolResultError = mcpsdk.NewToolResultError
)
