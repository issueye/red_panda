package state

import (
	"context"
	"fmt"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/plugins/core/state/internal"
	ptools "redpanda/protocol/tools"
)

type StateExecutor func(context.Context, *registry.ToolContext, string, map[string]any) (*ptools.Result, error)

type Dependencies struct {
	Todo   StateExecutor
	Memory StateExecutor
}

func Register(reg *registry.Registry, bus *hooks.ExtensionBus, deps Dependencies) []string {
	stateHandler := func(execute StateExecutor, name string) registry.HandlerFunc {
		return func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
			if execute == nil {
				return nil, fmt.Errorf("state executor for %s is unavailable", name)
			}
			return execute(ctx, tc, name, args)
		}
	}
	names := []string{
		"todo.write", "todo.list",
		"memory.list", "memory.create", "memory.update", "memory.delete",
		"web.search", "web.fetch",
	}

	entries := []registry.ToolEntry{
		{
			Definition: ptools.Definition{
				Name:        "todo.write",
				DisplayName: "Update todos",
				Description: "Optional session checklist for multi-step chat work. Prefer a full list each call and at most one in_progress.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"todos": map[string]any{
							"type":        "array",
							"description": "Task list entries. Prefer full active plan each call.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"id":          map[string]any{"type": "string", "description": "Short stable key or Gateway id from a prior result."},
									"client_key":  map[string]any{"type": "string", "description": "Optional explicit short key."},
									"content":     map[string]any{"type": "string", "description": "Short actionable task text."},
									"status":      map[string]any{"type": "string", "description": "pending | in_progress | completed | cancelled"},
									"priority":    map[string]any{"type": "string", "description": "low | medium | high"},
									"active_form": map[string]any{"type": "string", "description": "Optional present continuous UI form while in_progress."},
								},
								"required": []string{"content", "status"},
							},
						},
						"merge": map[string]any{"type": "boolean", "description": "true (default): keep items not listed. false: replace entire list."},
					},
					"required": []string{"todos"},
				},
			},
			Handler:      stateHandler(deps.Todo, "todo.write"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "todo.list",
				DisplayName: "List todos",
				Description: "Read the current session checklist (todo.*). Prefer injected Todo context after writes. Not durable memory.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{"type": "string", "description": "all|open|pending|in_progress|completed|cancelled"},
						"limit":  map[string]any{"type": "integer"},
					},
				},
			},
			Handler:      stateHandler(deps.Todo, "todo.list"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "memory.list",
				DisplayName: "List memory",
				Description: "List durable project/session preferences and facts. Not for the live work checklist (todo.*).",
				Risk:        ptools.RiskMedium,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scope":  map[string]any{"type": "string", "description": "Optional memory scope: project or session."},
						"status": map[string]any{"type": "string", "description": "Optional status: active or disabled."},
						"limit":  map[string]any{"type": "integer", "description": "Maximum number of records to return."},
					},
				},
			},
			Handler:      stateHandler(deps.Memory, "memory.list"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "memory.create",
				DisplayName: "Create memory",
				Description: "Create durable project/session knowledge for future runs (preferences, standing facts). Do NOT use as a task queue (todo.write).",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"scope":      map[string]any{"type": "string", "description": "Memory scope: project or session."},
						"kind":       map[string]any{"type": "string", "description": "Memory kind."},
						"title":      map[string]any{"type": "string", "description": "Short title."},
						"content":    map[string]any{"type": "string", "description": "Memory content."},
						"confidence": map[string]any{"type": "string", "description": "Confidence: low, medium, or high."},
					},
					"required": []string{"scope", "content"},
				},
			},
			Handler:      stateHandler(deps.Memory, "memory.create"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "memory.update",
				DisplayName: "Update memory",
				Description: "Update a project or session memory record visible to the current run.",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         map[string]any{"type": "string", "description": "Memory record id."},
						"kind":       map[string]any{"type": "string", "description": "Memory kind."},
						"status":     map[string]any{"type": "string", "description": "Status: active or disabled."},
						"title":      map[string]any{"type": "string", "description": "Short title."},
						"content":    map[string]any{"type": "string", "description": "Memory content."},
						"confidence": map[string]any{"type": "string", "description": "Confidence: low, medium, or high."},
					},
					"required": []string{"id"},
				},
			},
			Handler:      stateHandler(deps.Memory, "memory.update"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "memory.delete",
				DisplayName: "Delete memory",
				Description: "Soft-delete a project or session memory record visible to the current run.",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "string", "description": "Memory record id."},
					},
					"required": []string{"id"},
				},
			},
			Handler:      stateHandler(deps.Memory, "memory.delete"),
			TimeoutClass: registry.GatewayToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "web.search",
				DisplayName: "Web search",
				Description: "Search the public web and return titles, URLs, and snippets. Uses Tavily when configured (recommended), otherwise DuckDuckGo. May include a short answer field when Tavily is used.",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":       map[string]any{"type": "string", "description": "Search query text."},
						"max_results": map[string]any{"type": "integer", "description": "Maximum number of results to return."},
					},
					"required": []string{"query"},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				return internal.HandlerWebSearch(ctx, tc, args)
			},
			TimeoutClass: registry.SelfManagedToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
		{
			Definition: ptools.Definition{
				Name:        "web.fetch",
				DisplayName: "Web fetch",
				Description: "Download a single http or https URL and return readable text from the page.",
				Risk:        ptools.RiskHigh,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":       map[string]any{"type": "string", "description": "Absolute http or https URL to fetch."},
						"max_bytes": map[string]any{"type": "integer", "description": "Maximum response body size in bytes."},
					},
					"required": []string{"url"},
				},
			},
			Handler: func(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				return internal.HandlerWebFetch(ctx, tc, args)
			},
			TimeoutClass: registry.SelfManagedToolTimeout,
			OpsOnly:      false,
			Source:       "builtin:state",
		},
	}

	for _, e := range entries {
		reg.MustRegister(e)
	}

	return names
}
