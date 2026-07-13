package tools

import "strings"

// IsMCPToolName reports whether name is a Runtime-registered MCP tool (docs/19).
func IsMCPToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}
