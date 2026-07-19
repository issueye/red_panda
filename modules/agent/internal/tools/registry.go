package tools

import (
	"strings"
	"time"

	ptools "redpanda/protocol/tools"
)

// defaultLocalToolTimeout is the hard limit for local filesystem and RPC tools
// that do not manage their own deadlines.
const defaultLocalToolTimeout = 30 * time.Second

// defaultGatewayToolTimeout limits memory/todo/goal/context callbacks to Gateway.
const defaultGatewayToolTimeout = 30 * time.Second

type toolTimeoutClass uint8

const (
	localToolTimeout toolTimeoutClass = iota
	gatewayToolTimeout
	selfManagedToolTimeout
)

type stableToolSpec struct {
	definition   ptools.Definition
	timeoutClass toolTimeoutClass
}

var stableToolTimeoutByName = func() map[string]time.Duration {
	registry := stableToolRegistry()
	timeouts := make(map[string]time.Duration, len(registry))
	for _, spec := range registry {
		timeouts[spec.definition.Name] = spec.timeoutClass.duration()
	}
	return timeouts
}()

// stableToolDefinitions returns the provider-visible tool surface in the stable
// public order locked by TestStableToolRegistryPreservesPublicOrder.
// Tool schemas live in domain-specific files (defs_workspace.go, defs_coding.go,
// defs_orchestration.go, defs_state.go, defs_web.go) and are aggregated here
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C2).
func stableToolDefinitions() []ptools.Definition {
	defs := make([]ptools.Definition, 0, 64)
	defs = append(defs, workspaceToolDefinitions()...)
	defs = append(defs, codingToolDefinitions()...)
	defs = append(defs, orchestrationToolDefinitions()...)
	// stateToolDefinitions historically interleaves todo/goal/memory/web/context in
	// this exact order; web sits between memory and context (locked by TestStableToolRegistryPreservesPublicOrder).
	defs = append(defs, stateToolDefinitions()...)
	return defs
}

func stableToolRegistry() []stableToolSpec {
	definitions := stableToolDefinitions()
	registry := make([]stableToolSpec, 0, len(definitions))
	for _, definition := range definitions {
		registry = append(registry, stableToolSpec{
			definition:   definition,
			timeoutClass: timeoutClassForStableTool(definition.Name),
		})
	}
	return registry
}

func timeoutClassForStableTool(name string) toolTimeoutClass {
	switch {
	case name == "shell.exec",
		name == "skill.run",
		name == "worker.delegate",
		name == "worker.cancel",
		name == "worker.send",
		name == "worker.receive",
		strings.HasPrefix(name, "web."):
		return selfManagedToolTimeout
	case strings.HasPrefix(name, "memory."),
		strings.HasPrefix(name, "todo."),
		strings.HasPrefix(name, "goal."),
		strings.HasPrefix(name, "context."):
		// All four state domains are Gateway-mediated RPC tools (docs/41 W0-2).
		return gatewayToolTimeout
	default:
		return localToolTimeout
	}
}

func (ToolRunner) AvailableTools() []ptools.Definition {
	registry := stableToolRegistry()
	definitions := make([]ptools.Definition, 0, len(registry))
	for _, spec := range registry {
		definitions = append(definitions, spec.definition)
	}
	return definitions
}

func stableToolTimeoutFor(name string) (time.Duration, bool) {
	timeout, ok := stableToolTimeoutByName[name]
	return timeout, ok
}

func (class toolTimeoutClass) duration() time.Duration {
	switch class {
	case gatewayToolTimeout:
		return defaultGatewayToolTimeout
	case selfManagedToolTimeout:
		return 0
	default:
		return defaultLocalToolTimeout
	}
}
