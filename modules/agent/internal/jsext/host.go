package jsext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/pluginmeta"
	ptools "redpanda/protocol/tools"
)

const (
	defaultCallTimeout = 5 * time.Second
	maxSourceBytes     = 1 << 20
	maxResultBytes     = 1 << 20
)

type Host struct {
	registry *registry.Registry
	bus      *hooks.ExtensionBus
	log      io.Writer
	timeout  time.Duration
}

type Plugin struct {
	ID         string
	Path       string
	Generation uint64
	source     string
	vm         *goja.Runtime
	mu         sync.Mutex
	tools      []pendingTool
	hooks      []pendingHook
}

type Diagnostic struct {
	Path  string
	Error string
}

type toolDefinition struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	Risk        string         `json:"risk"`
	Parameters  map[string]any `json:"parameters"`
	OpsOnly     bool           `json:"opsOnly"`
}

type pendingTool struct {
	definition toolDefinition
	handler    goja.Callable
}

type pendingHook struct {
	name    hooks.HookName
	order   hooks.HookOrder
	handler goja.Callable
}

func NewHost(reg *registry.Registry, bus *hooks.ExtensionBus, log io.Writer) *Host {
	if reg == nil {
		reg = registry.NewRegistry()
	}
	if bus == nil {
		bus = hooks.NewBus()
	}
	if log == nil {
		log = io.Discard
	}
	return &Host{registry: reg, bus: bus, log: log, timeout: defaultCallTimeout}
}

func (h *Host) LoadFile(ctx context.Context, id, path string, generation uint64) (*Plugin, error) {
	if err := pluginmeta.ValidateSource("js:" + id); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSourceBytes {
		return nil, fmt.Errorf("javascript plugin exceeds %d bytes", maxSourceBytes)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	plugin := &Plugin{ID: id, Path: path, Generation: generation, source: "js:" + id, vm: goja.New()}
	plugin.vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	if err := h.installAPI(plugin); err != nil {
		return nil, err
	}
	plugin.mu.Lock()
	_, err = plugin.run(ctx, h.timeout, func() (goja.Value, error) {
		return plugin.vm.RunScript(path, string(source))
	})
	plugin.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("load javascript plugin %s: %w", id, err)
	}
	if err := h.commit(plugin); err != nil {
		h.registry.ClearSource(plugin.source)
		h.bus.ClearSource(plugin.source)
		return nil, err
	}
	return plugin, nil
}

func (h *Host) Unload(plugin *Plugin) {
	if plugin == nil {
		return
	}
	h.registry.ClearSource(plugin.source)
	h.bus.ClearSource(plugin.source)
}

func (h *Host) DiscoverAndLoad(ctx context.Context, root string) ([]*Plugin, []Diagnostic) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []Diagnostic{{Path: root, Error: err.Error()}}
	}
	sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) })
	var loaded []*Plugin
	var diagnostics []Diagnostic
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".js") {
			continue
		}
		id := strings.TrimSuffix(strings.ToLower(entry.Name()), filepath.Ext(entry.Name()))
		path := filepath.Join(root, entry.Name())
		plugin, err := h.LoadFile(ctx, id, path, 1)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Error: err.Error()})
			continue
		}
		loaded = append(loaded, plugin)
	}
	return loaded, diagnostics
}

func (h *Host) installAPI(plugin *Plugin) error {
	rp := plugin.vm.NewObject()
	if err := rp.Set("registerTool", func(call goja.FunctionCall) goja.Value {
		var definition toolDefinition
		if err := plugin.vm.ExportTo(call.Argument(0), &definition); err != nil {
			panic(plugin.vm.NewTypeError("invalid tool definition: %v", err))
		}
		handler, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(plugin.vm.NewTypeError("tool handler must be a function"))
		}
		if err := validateToolDefinition(definition); err != nil {
			panic(plugin.vm.NewTypeError("%v", err))
		}
		plugin.tools = append(plugin.tools, pendingTool{definition: definition, handler: handler})
		return goja.Undefined()
	}); err != nil {
		return err
	}
	if err := rp.Set("on", func(call goja.FunctionCall) goja.Value {
		name := hooks.HookName(call.Argument(0).String())
		if err := pluginmeta.ValidateHookName(string(name)); err != nil {
			panic(plugin.vm.NewTypeError("%v", err))
		}
		handler, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(plugin.vm.NewTypeError("hook handler must be a function"))
		}
		order := hooks.HookOrderPlugin
		if options := call.Argument(2); !goja.IsUndefined(options) && !goja.IsNull(options) {
			if value := options.ToObject(plugin.vm).Get("order"); !goja.IsUndefined(value) {
				order = hooks.HookOrder(value.ToInteger())
			}
		}
		plugin.hooks = append(plugin.hooks, pendingHook{name: name, order: order, handler: handler})
		return goja.Undefined()
	}); err != nil {
		return err
	}
	if err := rp.Set("log", func(call goja.FunctionCall) goja.Value {
		level := call.Argument(0).String()
		message := call.Argument(1).String()
		fmt.Fprintf(h.log, "js plugin log: plugin=%s level=%s message=%s\n", plugin.ID, level, message)
		return goja.Undefined()
	}); err != nil {
		return err
	}
	return plugin.vm.Set("rp", rp)
}

func (h *Host) commit(plugin *Plugin) error {
	for _, tool := range plugin.tools {
		definition := tool.definition
		handler := tool.handler
		_, err := h.registry.Register(registry.ToolEntry{
			Definition: ptools.Definition{
				Name: definition.Name, DisplayName: definition.DisplayName, Description: definition.Description,
				Risk: ptools.Risk(definition.Risk), Parameters: definition.Parameters,
			},
			Handler: func(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				return h.callTool(ctx, plugin, handler, definition.Name, toolCtx, args)
			},
			TimeoutClass: registry.SelfManagedToolTimeout,
			OpsOnly:      definition.OpsOnly,
			Source:       plugin.source,
		})
		if err != nil {
			return fmt.Errorf("register javascript tool %s: %w", definition.Name, err)
		}
	}
	for _, hook := range plugin.hooks {
		handler := hook.handler
		if err := h.bus.Register(hook.name, hook.order, plugin.source, func(ctx *hooks.HookContext, event map[string]any) *hooks.HookResult {
			return h.callHook(plugin, handler, ctx, event)
		}); err != nil {
			return fmt.Errorf("register javascript hook %s: %w", hook.name, err)
		}
	}
	return nil
}

func (h *Host) callTool(ctx context.Context, plugin *Plugin, handler goja.Callable, name string, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	value, err := plugin.run(ctx, h.timeout, func() (goja.Value, error) {
		return handler(goja.Undefined(), plugin.vm.ToValue(args), plugin.vm.ToValue(toolContextMap(toolCtx)))
	})
	if err != nil {
		return nil, fmt.Errorf("javascript plugin %s tool %s: %w", plugin.ID, name, err)
	}
	exported := value.Export()
	result := &ptools.Result{Name: name, Status: ptools.CallStatusCompleted}
	switch output := exported.(type) {
	case string:
		result.Output = output
	case map[string]any:
		if err := mapResult(output, result); err != nil {
			return nil, err
		}
	case nil:
	default:
		encoded, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("javascript plugin %s returned unsupported result", plugin.ID)
		}
		result.Output = string(encoded)
	}
	if len(result.Output)+len(result.Error) > maxResultBytes {
		return nil, fmt.Errorf("javascript plugin %s result exceeds %d bytes", plugin.ID, maxResultBytes)
	}
	return result, nil
}

func (h *Host) callHook(plugin *Plugin, handler goja.Callable, hookCtx *hooks.HookContext, event map[string]any) *hooks.HookResult {
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	ctx := context.Background()
	if hookCtx != nil && hookCtx.Context != nil {
		ctx = hookCtx.Context
	}
	value, err := plugin.run(ctx, h.timeout, func() (goja.Value, error) {
		return handler(goja.Undefined(), plugin.vm.ToValue(event), plugin.vm.ToValue(hookContextMap(hookCtx)))
	})
	if err != nil {
		panic(fmt.Errorf("javascript plugin %s hook failed: %w", plugin.ID, err))
	}
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	result, ok := value.Export().(map[string]any)
	if !ok {
		panic(fmt.Errorf("javascript plugin %s hook result must be an object", plugin.ID))
	}
	return hookResult(result)
}

func (p *Plugin) run(parent context.Context, timeout time.Duration, execute func() (goja.Value, error)) (goja.Value, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	p.vm.ClearInterrupt()
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			p.vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	value, err := execute()
	close(done)
	<-watcherDone
	cancel()
	p.vm.ClearInterrupt()
	return value, err
}

func validateToolDefinition(definition toolDefinition) error {
	if err := pluginmeta.ValidateToolName(definition.Name); err != nil {
		return err
	}
	switch definition.Risk {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("tool %s has invalid risk %q", definition.Name, definition.Risk)
	}
	return nil
}

func toolContextMap(ctx *registry.ToolContext) map[string]any {
	if ctx == nil {
		return nil
	}
	return map[string]any{"toolCallId": ctx.ToolCallID, "runId": ctx.RunID, "sessionId": ctx.SessionID, "workingDir": ctx.WorkingDir}
}

func hookContextMap(ctx *hooks.HookContext) map[string]any {
	if ctx == nil {
		return nil
	}
	return map[string]any{"runId": ctx.RunID, "sessionId": ctx.SessionID, "messageId": ctx.MessageID, "cwd": ctx.CWD, "mode": ctx.Mode, "hasUI": ctx.HasUI}
}

func mapResult(value map[string]any, result *ptools.Result) error {
	if output, ok := value["output"].(string); ok {
		result.Output = output
	}
	if message, ok := value["error"].(string); ok && message != "" {
		result.Error = message
		result.Status = ptools.CallStatusFailed
	}
	if exitCode, ok := value["exitCode"].(int64); ok {
		result.ExitCode = int(exitCode)
	}
	return nil
}

func hookResult(value map[string]any) *hooks.HookResult {
	result := &hooks.HookResult{}
	result.Block, _ = value["block"].(bool)
	result.Cancel, _ = value["cancel"].(bool)
	result.Reason, _ = value["reason"].(string)
	result.Transform, _ = value["transform"].(map[string]any)
	return result
}
