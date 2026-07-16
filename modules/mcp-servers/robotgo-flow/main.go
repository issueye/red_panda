// robotgo-flow MCP stdio server for red_panda.
//
// Built on github.com/mark3labs/mcp-go via redpanda/mcpkit.
// Execution shells out to robotgo-flow.exe (set ROBOTGO_FLOW_COMMAND).
//
// Register in Gateway Settings → MCP:
//
//	name: robotgo_flow
//	command: <path>/bin/mcp-robotgo-flow.exe
//	env: ROBOTGO_FLOW_COMMAND=<path>/robotgo-flow.exe
//	timeouts.call_ms: 600000  (RPA runs can be long)
package main

import (
	"fmt"
	"os"

	"redpanda/mcpkit"
)

func main() {
	s := mcpkit.NewServer("robotgo-flow", "0.1.0")
	registerTools(s)
	if err := mcpkit.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-robotgo-flow: %v\n", err)
		os.Exit(1)
	}
}
