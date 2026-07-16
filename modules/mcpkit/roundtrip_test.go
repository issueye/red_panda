package mcpkit

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

func TestServerToolRoundTripInProcess(t *testing.T) {
	s := NewServer("roundtrip", "0.0.1")
	AddTextTool(s, NewTool("echo",
		WithDescription("Echo a message"),
		WithString("msg", Required(), Description("message to echo")),
	), func(ctx context.Context, args map[string]any) (string, bool) {
		return "echo:" + stringArg(args, "msg"), false
	})

	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	initReq := mcpsdk.InitializeRequest{}
	initReq.Params.ProtocolVersion = DefaultProtocolVersion
	initReq.Params.ClientInfo = mcpsdk.Implementation{Name: "test", Version: "0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(ctx, mcpsdk.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", tools.Tools)
	}

	call := mcpsdk.CallToolRequest{}
	call.Params.Name = "echo"
	call.Params.Arguments = map[string]any{"msg": "hi"}
	result, err := c.CallTool(ctx, call)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", result)
	}
	if got := mcpsdk.GetTextFromContent(result.Content[0]); got != "echo:hi" {
		t.Fatalf("result text = %q", got)
	}
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}
