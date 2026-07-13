package tools

import "strings"

// IsMCPToolName 判断 name 是否为 Runtime 已注册的 MCP 工具（文档 19）。
func IsMCPToolName(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}
