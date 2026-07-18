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

func stableToolDefinitions() []ptools.Definition {
	return []ptools.Definition{
		{
			Name:        "workspace.read_file",
			DisplayName: "Read file",
			Description: "Read a text file inside the active workspace.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Workspace-relative file path."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "workspace.list",
			DisplayName: "List files",
			Description: "List files and directories inside the active workspace.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative directory or file path."},
					"max_depth": map[string]any{"type": "integer", "description": "Maximum directory depth to include."},
				},
			},
		},
		{
			Name:        "workspace.stats",
			DisplayName: "Workspace stats",
			Description: "Summarize directory structure and file counts for split planning. Root agent MUST call this before multi-area analysis. Use total_files / top_level[].files to set each worker.delegate file_count or max_turns using formula max_turns = file_count + summary_turns (summary_turns is included as suggested_max_turns).",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Workspace-relative directory path. Defaults to workspace root."},
					"max_depth": map[string]any{"type": "integer", "description": "How deep to walk when counting (default 4, max 8)."},
				},
			},
		},
		{
			Name:        "workspace.grep",
			DisplayName: "Search files",
			Description: "Search text files inside the active workspace with a regular expression.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Regular expression to search for."},
					"path":        map[string]any{"type": "string", "description": "Workspace-relative directory or file path."},
					"max_matches": map[string]any{"type": "integer", "description": "Maximum number of matches to return."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "workspace.find_files",
			DisplayName: "Find files",
			Description: "Find files by filename, substring, or glob pattern inside the active workspace. Skips generated and dependency directories.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":     map[string]any{"type": "string", "description": "Filename substring or glob such as *.go or modules/**/*.jsx."},
					"path":        map[string]any{"type": "string", "description": "Workspace-relative directory to search. Defaults to workspace root."},
					"max_results": map[string]any{"type": "integer", "description": "Maximum matching paths to return (default 200, max 1000)."},
				},
				"required": []string{"pattern"},
			},
		},
		{
			Name:        "workspace.read_files",
			DisplayName: "Read files",
			Description: "Read up to 12 text files from the active workspace in one call, with clear per-file headings.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type": "array", "description": "Workspace-relative text file paths.",
						"items": map[string]any{"type": "string"}, "maxItems": defaultReadFiles,
					},
				},
				"required": []string{"paths"},
			},
		},
		{
			Name:        "workspace.write_file",
			DisplayName: "Write file",
			Description: "Write text content to a file inside the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"content": map[string]any{"type": "string", "description": "Text content to write."},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "workspace.edit_file",
			DisplayName: "Edit file",
			Description: "Replace exact text inside a file in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to replace."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences instead of requiring exactly one match."},
				},
				"required": []string{"path", "old_text", "new_text"},
			},
		},
		{
			Name:        "workspace.diff_file",
			DisplayName: "Preview diff",
			Description: "Preview a unified diff for a proposed text change to one workspace file.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string", "description": "Workspace-relative file path."},
					"content":     map[string]any{"type": "string", "description": "Full proposed file content."},
					"old_text":    map[string]any{"type": "string", "description": "Exact text to replace for preview."},
					"new_text":    map[string]any{"type": "string", "description": "Replacement text for preview."},
					"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences when previewing old_text/new_text."},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "workspace.apply_patch",
			DisplayName: "Apply patch",
			Description: "Apply a workspace-scoped unified patch to text files.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"patch": map[string]any{"type": "string", "description": "Unified diff patch to apply inside the workspace."},
				},
				"required": []string{"patch"},
			},
		},
		{
			Name:        "git.status",
			DisplayName: "Git status",
			Description: "Show the current branch and concise working tree status without modifying the repository.",
			Risk:        ptools.RiskLow,
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "git.diff",
			DisplayName: "Git diff",
			Description: "Show an unstaged, staged, or revision-based Git diff, optionally limited to one workspace path.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"staged":   map[string]any{"type": "boolean", "description": "Show staged changes."},
					"revision": map[string]any{"type": "string", "description": "Optional revision or range such as HEAD~1 or main...HEAD."},
					"path":     map[string]any{"type": "string", "description": "Optional workspace-relative path."},
				},
			},
		},
		{
			Name:        "git.log",
			DisplayName: "Git log",
			Description: "Show recent commits in a concise format, optionally limited to one workspace path.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max_count": map[string]any{"type": "integer", "description": "Maximum commits to return (default 10, max 50)."},
					"path":      map[string]any{"type": "string", "description": "Optional workspace-relative path."},
				},
			},
		},
		{
			Name:        "git.show",
			DisplayName: "Git show",
			Description: "Show commit metadata and patch for a revision, optionally limited to one workspace path.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"revision": map[string]any{"type": "string", "description": "Revision to show. Defaults to HEAD."},
					"path":     map[string]any{"type": "string", "description": "Optional workspace-relative path."},
				},
			},
		},
		{
			Name:        "shell.exec",
			DisplayName: "Shell",
			Description: "Run a shell command in the active workspace.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "Command to execute."},
				},
				"required": []string{"command"},
			},
		},
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
