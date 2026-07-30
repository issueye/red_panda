package runtime

// Runtime facade: construction, Serve loop, and JSON-RPC method dispatch.
// Handlers: runtime_handlers.go; lifecycle: runtime_lifecycle.go; events: runtime_events.go (docs/41 W5-5).

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"

	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/worker"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
)

const (
	defaultProviderToolTurns = 12
	maxProviderToolTurnsCap  = 48
	maxJSONRPCLineBytes      = 4 * 1024 * 1024
)

// effectiveProviderToolTurns 返回单次回复中提供方与工具循环的预算。
func effectiveProviderToolTurns(options methods.ReplyOptions) int {
	if options.MaxToolTurns > 0 {
		if options.MaxToolTurns > maxProviderToolTurnsCap {
			return maxProviderToolTurnsCap
		}
		return options.MaxToolTurns
	}
	if env := os.Getenv("RED_PANDA_MAX_TOOL_TURNS"); env != "" {
		if parsed, err := strconv.Atoi(env); err == nil && parsed > 0 {
			if parsed > maxProviderToolTurnsCap {
				return maxProviderToolTurnsCap
			}
			return parsed
		}
	}
	return defaultProviderToolTurns
}

type Runtime struct {
	in      io.Reader
	out     io.Writer
	log     io.Writer
	version string

	mu              sync.Mutex
	eventMu         sync.Mutex
	initialized     bool
	protocolVersion string
	runStates       RunStateStore
	gatewayPending  map[jsonrpc.ID]chan jsonrpc.Response
	nextGatewayID   uint64
	permissions     map[string]chan permission.ResolveParams
	provider        provider.Provider
	workerPool      *worker.Pool
	mcp             *agentmcp.Manager

	// Plugin system (v0.3.0 — docs/53)
	registry   *registry.Registry
	bus        *hooks.ExtensionBus
	dispatcher *registry.Dispatcher
}

// Dependencies contains the replaceable collaborators used by Runtime.
type Dependencies struct {
	Provider provider.Provider
}

func New(in io.Reader, out io.Writer, log io.Writer, version string) *Runtime {
	return NewWithDependencies(in, out, log, version, Dependencies{})
}

// NewWithDependencies constructs a Runtime with explicitly supplied collaborators.
// Missing dependencies retain the same environment-backed defaults used by New.
func NewWithDependencies(in io.Reader, out io.Writer, log io.Writer, version string, deps Dependencies) *Runtime {
	modelProvider := deps.Provider
	if modelProvider == nil {
		modelProvider = provider.NewFromEnv(log)
	}
	rt := &Runtime{
		in:             in,
		out:            out,
		log:            log,
		version:        version,
		gatewayPending: map[jsonrpc.ID]chan jsonrpc.Response{},
		permissions:    map[string]chan permission.ResolveParams{},
		mcp:            agentmcp.NewManager(version, log),
		provider:       modelProvider,
	}
	// Plugin system (v0.3.0)
	rt.bus = hooks.NewBus()
	rt.registry = registry.NewRegistry()
	initCorePlugins(rt.registry, rt.bus)
	rt.dispatcher = initRPCDispatcher(rt)
	workerPool, err := worker.NewPool(worker.Config{Size: workerPoolSizeFromEnv()}, func(workerID worker.WorkerID) (worker.Executor, error) {
		return newLazyProcessExecutor(rt, workerID), nil
	})
	if err != nil {
		panic(fmt.Errorf("create WorkerPool: %w", err))
	}
	rt.workerPool = workerPool
	return rt
}

func (r *Runtime) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(r.in)
	scanner.Buffer(make([]byte, 64*1024), maxJSONRPCLineBytes)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := r.handleLine(ctx, line); err != nil {
			fmt.Fprintf(r.log, "handle json-rpc line: %v\n", err)
		}
	}
	return scanner.Err()
}

func (r *Runtime) handleLine(ctx context.Context, line []byte) error {
	var probe struct {
		JSONRPC string     `json:"jsonrpc"`
		ID      jsonrpc.ID `json:"id"`
		Method  string     `json:"method"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return r.writeResponse(jsonrpc.NewError("rpc-parse-error", -32700, "parse error"))
	}
	if probe.Method == "" && probe.ID != "" {
		return r.handleGatewayResponse(line)
	}

	var req jsonrpc.Request
	_ = json.Unmarshal(line, &req)
	if req.JSONRPC != jsonrpc.Version || req.Method == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32600, "invalid request"))
	}

	resp, err := r.dispatcher.Dispatch(ctx, req)
	if err != nil {
		return err
	}
	// Non-empty response means the handler produced a result (either success or
	// the Dispatcher's method-not-found error response).
	if resp.JSONRPC != "" {
		return r.writeResponse(resp)
	}
	return nil
}
