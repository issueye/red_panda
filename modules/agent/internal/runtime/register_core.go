package runtime

// register_core.go — P1 core plugin registration for the ToolRegistry.
// Replaces the ToolRunner executors and dispatch switch (docs/53 §7.1).
//
// All 4 P1 plugin domains are registered here at Runtime construction time.
// Plugin handlers use the registry.HandlerFunc signature defined in
// modules/agent/internal/runtime/registry/registry.go.

import (
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	coding "redpanda/agent/plugins/core/coding"
	orchestration "redpanda/agent/plugins/core/orchestration"
	state "redpanda/agent/plugins/core/state"
	workspace "redpanda/agent/plugins/core/workspace"
)

func initCorePlugins(reg *registry.Registry, bus *hooks.ExtensionBus) {
	// Register order determines the public tool order returned by Definitions().
	// (Locked by TestStableToolRegistryPreservesPublicOrder.)
	workspace.Register(reg, bus)
	coding.Register(reg, bus)
	orchestration.Register(reg, bus)
	state.Register(reg, bus)
}
