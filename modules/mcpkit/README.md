# mcpkit

Thin wrappers around [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) for red_panda.

## Packages

| Surface | API | Used by |
| --- | --- | --- |
| Client | `Open` / `Discover` / `Call` / `Tracker` | `modules/agent/internal/mcp` |
| Server | `NewServer` / `AddTextTool` / `ServeStdio` | `modules/mcp-servers/*` |

## Client (stdio)

```go
discovery := mcpkit.Discover(ctx, mcpkit.SessionConfig{
    Command: "my-mcp-server",
    Args:    []string{"--flag"},
    Env:     map[string]string{"KEY": "value"},
    Dir:     workspace,
}, mcpkit.DiscoverOptions{
    StartMS: 10_000, InitializeMS: 10_000, ListMS: 10_000,
    Tracker: tracker, // optional: interruptible via Tracker.CloseAll
})
```

`Call` starts a one-shot session: initialize → `tools/call` → close.

## Server (stdio)

```go
s := mcpkit.NewServer("demo", "1.0.0")
mcpkit.AddTextTool(s, mcpkit.NewTool("hello",
    mcpkit.WithDescription("Say hello"),
    mcpkit.WithString("name", mcpkit.Required()),
), func(ctx context.Context, args map[string]any) (string, bool) {
    return "hi " + fmt.Sprint(args["name"]), false
})
_ = mcpkit.ServeStdio(s)
```

## Boundaries

- **Gateway** still owns durable MCP config only (no process execution).
- **Agent Runtime** owns process lifecycle via `mcpkit` + `internal/mcp.Manager`.
- Protocol DTO types stay in `redpanda/protocol/mcp`.
