package tools

// defs_orchestration.go — skill.* and worker.* tool definitions
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C2).
// Order is preserved by the aggregator in registry.go.

import (
	ptools "redpanda/protocol/tools"
)

func orchestrationToolDefinitions() []ptools.Definition {
	return []ptools.Definition{
		{
			Name:        "skill.list",
			DisplayName: "List skills",
			Description: "List managed workspace skills under .codex/skills with name and description only. The catalog is also injected at conversation start; call this to re-check after skill.create/update/delete in the same run.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "skill.create",
			DisplayName: "Create skill",
			Description: "Create a managed SKILL.md under .codex/skills in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "Lowercase skill name using letters, digits, and hyphens."},
					"description":  map[string]any{"type": "string", "description": "Short description used to select the skill."},
					"instructions": map[string]any{"type": "string", "description": "Complete Markdown instructions for the skill."},
				},
				"required": []string{"name", "description", "instructions"},
			},
		},
		{
			Name:        "skill.update",
			DisplayName: "Update skill",
			Description: "Replace an existing managed SKILL.md under .codex/skills in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string", "description": "Existing lowercase skill name."},
					"description":  map[string]any{"type": "string", "description": "Replacement description used to select the skill."},
					"instructions": map[string]any{"type": "string", "description": "Complete replacement Markdown instructions."},
				},
				"required": []string{"name", "description", "instructions"},
			},
		},
		{
			Name:        "skill.delete",
			DisplayName: "Delete skill",
			Description: "Delete a managed SKILL.md under .codex/skills in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Existing lowercase skill name."},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "skill.run",
			DisplayName: "Run skill",
			Description: "Run a managed workspace skill in an isolated Worker Assignment and return only its final result. Prefer names from the skills catalog injected for this conversation.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Existing managed skill name."},
					"task": map[string]any{"type": "string", "description": "Task for the isolated skill Worker."},
				},
				"required": []string{"name", "task"},
			},
		},
		{
			Name:        "worker.delegate",
			DisplayName: "Delegate work",
			Description: "Delegate one focused task to an available Worker and return its assignment identity and final report. Delegated assignments cannot delegate again.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task":        map[string]any{"type": "string", "description": "Focused task with scope and expected output."},
					"profile_key": map[string]any{"type": "string", "description": "Optional Worker Profile key."},
					"max_turns":   map[string]any{"type": "integer", "minimum": 1, "description": "Optional tool-turn budget."},
				},
				"required": []string{"task"},
			},
		},
		{
			Name:        "worker.list",
			DisplayName: "List workers",
			Description: "List Worker slots and assignments for the current run.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"worker_id":     map[string]any{"type": "string"},
					"assignment_id": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "worker.cancel",
			DisplayName: "Cancel assignment",
			Description: "Cancel an assignment belonging to the current run.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"assignment_id": map[string]any{"type": "string"},
					"reason":        map[string]any{"type": "string"},
				},
				"required": []string{"assignment_id"},
			},
		},
		{
			Name:        "worker.pool_status",
			DisplayName: "Worker pool status",
			Description: "Inspect Worker capacity, health, and active Assignment counts.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "worker.send",
			DisplayName: "Send Worker message",
			Description: "Send a message to an active Worker Assignment in the current run.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to_worker_id":     map[string]any{"type": "string"},
					"to_assignment_id": map[string]any{"type": "string"},
					"kind":             map[string]any{"type": "string", "enum": []string{"request", "update", "result", "control"}},
					"payload":          map[string]any{"description": "JSON-compatible message payload."},
				},
				"required": []string{"to_worker_id", "kind", "payload"},
			},
		},
		{
			Name:        "worker.receive",
			DisplayName: "Receive Worker message",
			Description: "Wait for the next message addressed to the current active Assignment.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}
