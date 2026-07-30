package runtime

import (
	"context"
	"fmt"
	"strings"

	agentmcp "redpanda/agent/internal/mcp"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func (r *Runtime) registryForRun(runID string) *registry.Registry {
	r.runRegistryMu.RLock()
	scoped := r.runRegistries[runID]
	r.runRegistryMu.RUnlock()
	if scoped != nil {
		return scoped.Registry
	}
	return r.registry
}

func (r *Runtime) prepareRegistryForRun(ctx context.Context, params methods.ReplyParams) *registry.Registry {
	if scoped := r.existingRunRegistry(params.RunID); scoped != nil {
		return scoped.Registry
	}

	r.runRegistryPrepareMu.Lock()
	defer r.runRegistryPrepareMu.Unlock()
	if scoped := r.existingRunRegistry(params.RunID); scoped != nil {
		return scoped.Registry
	}

	scoped := registry.NewRunScopedRegistry(r.registry)
	if r.mcp != nil && len(params.Options.MCPServers) > 0 {
		if disabled := r.mcp.DisabledServers(); len(disabled) > 0 {
			fmt.Fprintf(r.log, "mcp disabled servers (crash budget): %s\n", strings.Join(disabled, ", "))
		}
		for _, definition := range r.mcp.PrepareToolsForRun(ctx, params) {
			binding, ok := r.mcp.Binding(params.RunID, definition.Name)
			if !ok {
				fmt.Fprintf(r.log, "mcp binding missing for discovered tool %s\n", definition.Name)
				continue
			}
			definition := definition
			source := "mcp:" + agentmcp.ServerID(binding.Config.Name)
			if _, err := scoped.Register(registry.ToolEntry{
				Definition:   definition,
				Source:       source,
				TimeoutClass: registry.SelfManagedToolTimeout,
				Handler: func(callCtx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*tools.Result, error) {
					output, err := r.mcp.ExecuteTool(callCtx, toolCtx.RunID, toolCtx.WorkingDir, tools.Call{
						ID: toolCtx.ToolCallID, Name: definition.Name, Arguments: args,
					})
					return runtimeToolResult(definition.Name, output, err), err
				},
			}); err != nil {
				fmt.Fprintf(r.log, "register MCP tool %s from %s: %v\n", definition.Name, source, err)
			}
		}
	}
	// JS entries are loaded globally but have higher per-run precedence than MCP.
	// Re-registering the active JS layer moves it above any same-name MCP entry.
	for _, entry := range r.registry.Entries() {
		if strings.HasPrefix(entry.Source, "js:") {
			if _, err := scoped.Register(entry); err != nil {
				fmt.Fprintf(r.log, "register run-scoped JS tool %s from %s: %v\n", entry.Definition.Name, entry.Source, err)
			}
		}
	}

	r.runRegistryMu.Lock()
	if r.runRegistries == nil {
		r.runRegistries = make(map[string]*registry.RunScopedRegistry)
	}
	r.runRegistries[params.RunID] = scoped
	r.runRegistryMu.Unlock()
	return scoped.Registry
}

func (r *Runtime) existingRunRegistry(runID string) *registry.RunScopedRegistry {
	r.runRegistryMu.RLock()
	defer r.runRegistryMu.RUnlock()
	return r.runRegistries[runID]
}

func (r *Runtime) clearRunRegistry(runID string) {
	r.runRegistryMu.Lock()
	delete(r.runRegistries, runID)
	r.runRegistryMu.Unlock()
	if r.mcp != nil {
		r.mcp.ClearBindings(runID)
	}
}
