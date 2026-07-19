package tools

// defs_state.go — todo.*, goal.*, memory.*, web.*, and context.* tool definitions
// (docs/plans/2026-07-19-convergence-wave.md Wave C Task C2). todo/goal/memory/context
// all flow through the Gateway-backed state.tool.execute RPC; web.* is grouped
// here because the historical public tool order interleaves it between memory.*
// and context.* (locked by TestStableToolRegistryPreservesPublicOrder).

import (
	ptools "redpanda/protocol/tools"
)

func stateToolDefinitions() []ptools.Definition {
	return []ptools.Definition{
		{
			Name:        "todo.write",
			DisplayName: "Update todos",
			Description: "Optional session checklist for non-Goal multi-step chat work. Goal execution uses goal.plan actions instead. Prefer a full list each call and at most one in_progress.",
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
		{
			Name:        "goal.create",
			DisplayName: "Create goal",
			Description: "Create and activate an outcome contract. Define independently assessable success criteria and optional constraints. Use only when no Goal is already bound.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":       map[string]any{"type": "string"},
					"objective":   map[string]any{"type": "string"},
					"strategy":    map[string]any{"type": "string"},
					"constraints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"criteria": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "properties": map[string]any{
							"id": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"},
						}, "required": []string{"description"},
					}},
					"max_iterations": map[string]any{"type": "integer"},
					"max_stagnation": map[string]any{"type": "integer"},
				},
				"required": []string{"objective", "criteria"},
			},
		},
		{
			Name:        "goal.plan",
			DisplayName: "Plan goal actions",
			Description: "Choose or revise the Goal strategy and Goal-owned action queue based on current evidence. The queue is adaptive, not a fixed up-front phase plan; at most one action may be active.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":  map[string]any{"type": "string"},
					"strategy": map[string]any{"type": "string"},
					"decision": map[string]any{"type": "string", "description": "Why this is the best next plan given current evidence."},
					"actions": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "properties": map[string]any{
							"key": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
							"description": map[string]any{"type": "string"}, "acceptance": map[string]any{"type": "string"},
							"status": map[string]any{"type": "string", "description": "queued | active | done | blocked | dropped"},
						}, "required": []string{"title", "acceptance"},
					}},
				},
				"required": []string{"goal_id"},
			},
		},
		{
			Name:        "goal.observe",
			DisplayName: "Record goal observation",
			Description: "Record what actually happened after an action. Done or blocked actions require concrete evidence. This records facts; use goal.assess separately to decide what they mean for the outcome.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":       map[string]any{"type": "string"},
					"action_id":     map[string]any{"type": "string", "description": "Goal action id or key; defaults to the active action."},
					"action_status": map[string]any{"type": "string", "description": "active | done | blocked | dropped"},
					"observation":   map[string]any{"type": "string"},
					"evidence":      map[string]any{"type": "string"},
				},
				"required": []string{"goal_id", "observation"},
			},
		},
		{
			Name:        "goal.assess",
			DisplayName: "Assess goal outcome",
			Description: "Close one controller iteration by assessing every success criterion against evidence and deciding whether the Goal is progressing, satisfied, blocked, or making no progress.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":       map[string]any{"type": "string"},
					"verdict":       map[string]any{"type": "string", "description": "progress | satisfied | blocked | no_progress"},
					"summary":       map[string]any{"type": "string"},
					"gap":           map[string]any{"type": "string"},
					"decision":      map[string]any{"type": "string", "description": "Next decision; required for progress."},
					"action_id":     map[string]any{"type": "string"},
					"action_status": map[string]any{"type": "string"},
					"evidence":      map[string]any{"type": "string"},
					"criteria": map[string]any{"type": "array", "items": map[string]any{
						"type": "object", "properties": map[string]any{
							"id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "description": "unknown | met | not_met | blocked"},
							"evidence": map[string]any{"type": "string"},
						}, "required": []string{"id", "status", "evidence"},
					}},
				},
				"required": []string{"goal_id", "verdict", "summary", "evidence", "criteria"},
			},
		},
		{
			Name:        "goal.finish",
			DisplayName: "Finish goal",
			Description: "Finalize the Goal with an outcome report. succeeded requires a persisted satisfied assessment in which every criterion is met with evidence.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object", "properties": map[string]any{
					"goal_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "description": "succeeded | failed"},
					"summary": map[string]any{"type": "string"}, "report_markdown": map[string]any{"type": "string"},
				}, "required": []string{"goal_id", "status", "summary", "report_markdown"},
			},
		},
		{
			Name:        "goal.list",
			DisplayName: "List goals",
			Description: "List session goals. Prefer injected Goal context when present.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "todo.list",
			DisplayName: "List todos",
			Description: "Read the current session checklist (todo.*). Prefer injected Todo context after writes. Not durable memory and not Goal scratchpad notes.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "description": "all|open|pending|in_progress|completed|cancelled"},
					"limit":  map[string]any{"type": "integer"},
				},
			},
		},
		{
			Name:        "memory.list",
			DisplayName: "List memory",
			Description: "List durable project/session preferences and facts (cross-goal). Not for the live work checklist (todo.*) or Goal execution notes (context.*).",
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
		{
			Name:        "memory.create",
			DisplayName: "Create memory",
			Description: "Create durable project/session knowledge for future runs (preferences, standing facts). Do NOT use as a task queue (todo.write) or to store Goal-only findings (context.write).",
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
		{
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
		{
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
		{
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
		{
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
		{
			Name:        "context.read",
			DisplayName: "Read goal notes",
			Description: "Read this Goal's shared scratchpad (findings/decisions/handoffs). Scoped to one goal; shared across segments, continues, and specialists. Not durable user preferences (memory.*) and not the step checklist (todo.*).",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":     map[string]any{"type": "string", "description": "Goal id whose notes to read."},
					"kind":        map[string]any{"type": "string", "description": "Optional kind filter: finding, decision, risk, fact, handoff, note."},
					"limit":       map[string]any{"type": "integer", "description": "Maximum number of notes to return."},
					"since_seq":   map[string]any{"type": "integer", "description": "Only return notes with seq greater than this value."},
					"pinned_only": map[string]any{"type": "boolean", "description": "Only return pinned notes."},
				},
				"required": []string{"goal_id"},
			},
		},
		{
			Name:        "context.search",
			DisplayName: "Search goal notes",
			Description: "Full-text search across the shared scratchpad notes for a goal (matches title and body).",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id": map[string]any{"type": "string", "description": "Goal id whose notes to search."},
					"query":   map[string]any{"type": "string", "description": "Search query text."},
					"limit":   map[string]any{"type": "integer", "description": "Maximum number of matches to return."},
				},
				"required": []string{"goal_id", "query"},
			},
		},
		{
			Name:        "context.write",
			DisplayName: "Write goal note",
			Description: "Append a structured note on the active Goal scratchpad (finding/decision/risk/fact/handoff). Survives segment boundaries and specialist handoffs. Controller progress belongs in goal.observe/goal.assess; use memory.create only for lasting preferences across goals.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id": map[string]any{"type": "string", "description": "Goal id to attach the note to."},
					"kind":    map[string]any{"type": "string", "description": "Note kind: finding, decision, risk, fact, handoff, note."},
					"title":   map[string]any{"type": "string", "description": "Short title."},
					"body":    map[string]any{"type": "string", "description": "Note content."},
					"pinned":  map[string]any{"type": "boolean", "description": "Pin this note so it is always injected into the goal context."},
				},
				"required": []string{"goal_id", "kind", "title", "body"},
			},
		},
		{
			Name:        "context.replace",
			DisplayName: "Replace goal note",
			Description: "Upsert a shared scratchpad note by (goal_id, kind, title). Updates the body in place when a matching note exists, otherwise creates one.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id": map[string]any{"type": "string", "description": "Goal id to attach the note to."},
					"kind":    map[string]any{"type": "string", "description": "Note kind: finding, decision, risk, fact, handoff, note."},
					"title":   map[string]any{"type": "string", "description": "Short title identifying the note to replace."},
					"body":    map[string]any{"type": "string", "description": "Replacement content."},
					"pinned":  map[string]any{"type": "boolean", "description": "Pin this note."},
				},
				"required": []string{"goal_id", "kind", "title", "body"},
			},
		},
		{
			Name:        "context.delete",
			DisplayName: "Delete goal note",
			Description: "Delete a shared scratchpad note from a goal.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id": map[string]any{"type": "string", "description": "Goal id the note belongs to."},
					"note_id": map[string]any{"type": "string", "description": "Note id to delete."},
				},
				"required": []string{"goal_id", "note_id"},
			},
		},
	}
}
