package tools

import (
	"context"

	ptools "redpanda/protocol/tools"
)

// dispatchWebTool handles web.search and web.fetch.
func dispatchWebTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (output string, handled bool, err error) {
	switch call.Name {
	case "web.search":
		searchOpts := effectiveWebSearchOptions(runCtx)
		out, e := runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebSearch(
				opCtx,
				StringArg(call.Arguments, "query"),
				effectiveWebResultCount(runCtx, IntArg(call.Arguments, "max_results", 0)),
				searchOpts,
			)
		})
		return out, true, e
	case "web.fetch":
		out, e := runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebFetch(
				opCtx,
				StringArg(call.Arguments, "url"),
				effectiveWebFetchBytes(runCtx, IntArg(call.Arguments, "max_bytes", 0)),
				effectiveWebHTTPProxy(runCtx),
			)
		})
		return out, true, e
	default:
		return "", false, nil
	}
}
