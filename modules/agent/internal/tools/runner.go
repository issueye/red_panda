package tools

import (
	"context"
	"fmt"
	"os"
	"redpanda/agent/internal/skill"
	"strings"
	"time"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

const maxToolOutputBytes = 64 * 1024

// defaultLocalToolTimeout is a hard upper bound for local FS/RPC tools that do not
// manage their own deadline. Normal reads fail in milliseconds; this only prevents
// stuck network mounts, pathological trees, or hung gateway RPCs from freezing a run.
const defaultLocalToolTimeout = 30 * time.Second

// defaultGatewayToolTimeout bounds memory/todo tools that call back into Gateway.
const defaultGatewayToolTimeout = 30 * time.Second

type MemoryToolExecutor func(context.Context, methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error)
type TodoToolExecutor func(context.Context, methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error)
type GoalToolExecutor func(context.Context, methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error)
type ContextToolExecutor func(context.Context, methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error)
type SkillRunExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)
type SubagentRunExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)
type MCPToolExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)

// SubagentManager exposes parent-agent control of specialists and the process pool.
type SubagentManager interface {
	List(runCtx ToolRunContext, call ptools.Call) (string, error)
	Cancel(runCtx ToolRunContext, call ptools.Call) (string, error)
	Reset(runCtx ToolRunContext, call ptools.Call) (string, error)
	PoolStatus() (string, error)
	PoolResize(call ptools.Call) (string, error)
	PoolReset() (string, error)
}

type ToolRunner struct {
	MemoryExecutor   MemoryToolExecutor
	TodoExecutor     TodoToolExecutor
	GoalExecutor     GoalToolExecutor
	ContextExecutor  ContextToolExecutor
	SkillExecutor    SkillRunExecutor
	SubagentExecutor SubagentRunExecutor
	SubagentManager  SubagentManager
	MCPExecutor      MCPToolExecutor
}

type ToolRunContext struct {
	WorkingDir string
	RunID      string
	SessionID  string
	Reply      *methods.ReplyParams
}

type ToolInvocation struct {
	Call ptools.Call
}

func (ToolRunner) AvailableTools() []ptools.Definition {
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
			Description: "Summarize directory structure and file counts for split planning. Root agent MUST call this before multi-area analysis. Use total_files / top_level[].files to set each subagent.run file_count or max_turns using formula max_turns = file_count + summary_turns (summary_turns is included as suggested_max_turns).",
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
			Description: "Run a managed workspace skill in an isolated runtime-process subagent and return only its final result. Prefer names from the skills catalog injected for this conversation.",
			Risk:        ptools.RiskHigh,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Existing managed skill name."},
					"task": map[string]any{"type": "string", "description": "Task for the isolated skill subagent."},
				},
				"required": []string{"name", "task"},
			},
		},
		{
			Name:        "subagent.run",
			DisplayName: "Run subagent",
			Description: "Spawn a process-pool specialist and return its final report. For Goal multi-step work use phase names exactly: goal-analyst, goal-planner, goal-implementer, goal-verifier, goal-evaluator (tool policy and prompts are applied automatically). For broad codebase analysis without Goal, call workspace.stats first and set file_count/max_turns; split non-overlapping scopes; synthesize results instead of re-reading.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task": map[string]any{"type": "string", "description": "Focused task for the subagent. Include scope, goals, and expected output."},
					"name": map[string]any{"type": "string", "description": "Specialist id. Goal pipeline: goal-analyst|goal-planner|goal-implementer|goal-verifier|goal-evaluator. Otherwise e.g. desktop-analyst."},
					"path": map[string]any{"type": "string", "description": "Workspace-relative directory this specialist owns. Used to count files when file_count/max_turns are omitted."},
					"file_count": map[string]any{
						"type":        "integer",
						"description": "Number of files in the specialist scope from workspace.stats. When set (and max_turns omitted), max_turns becomes file_count + summary turns.",
						"minimum":     0,
					},
					"max_turns": map[string]any{
						"type":        "integer",
						"description": "Optional explicit tool-turn budget. Prefer max_turns = file_count + summary turns from workspace.stats (suggested_max_turns). No fixed maximum.",
						"minimum":     1,
					},
				},
				"required": []string{"task"},
			},
		},
		{
			Name:        "subagent.list",
			DisplayName: "List subagents",
			Description: "List subagents for the current root run and show process-pool occupancy. Use this to manage specialists.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id":      map[string]any{"type": "string", "description": "Optional root run id. Defaults to the current run."},
					"subagent_id": map[string]any{"type": "string", "description": "Optional subagent id filter."},
				},
			},
		},
		{
			Name:        "subagent.cancel",
			DisplayName: "Cancel subagent",
			Description: "Cancel a running subagent managed by the parent agent.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{"type": "string", "description": "Subagent id to cancel."},
					"run_id":      map[string]any{"type": "string", "description": "Optional root run id. Defaults to the current run."},
				},
				"required": []string{"subagent_id"},
			},
		},
		{
			Name:        "subagent.reset",
			DisplayName: "Reset subagent",
			Description: "Cancel a subagent if it is still running, mark it reset, and remove it from the active registry so a fresh specialist can be started. Optionally also reset idle process-pool workers.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subagent_id": map[string]any{"type": "string", "description": "Subagent id to reset."},
					"run_id":      map[string]any{"type": "string", "description": "Optional root run id. Defaults to the current run."},
					"reset_pool":  map[string]any{"type": "boolean", "description": "If true, also discard idle process-pool workers."},
				},
				"required": []string{"subagent_id"},
			},
		},
		{
			Name:        "subagent.pool_status",
			DisplayName: "Subagent pool status",
			Description: "Inspect the subagent process pool: limit, active workers, idle workers, and in-use count.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "subagent.pool_resize",
			DisplayName: "Resize subagent pool",
			Description: "Change the subagent process pool size (1-8). Excess idle workers are closed immediately.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"size": map[string]any{"type": "integer", "description": "Desired pool size between 1 and 8."},
				},
				"required": []string{"size"},
			},
		},
		{
			Name:        "subagent.pool_reset",
			DisplayName: "Reset subagent pool",
			Description: "Discard all idle process-pool workers so the next subagent.run creates fresh processes. In-use workers are left running until completion.",
			Risk:        ptools.RiskMedium,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "todo.write",
			DisplayName: "Update todos",
			Description: "Session checklist only (this chat). REQUIRED for multi-step work: plan steps shown above the chat input. Prefer full list each call; at most one in_progress. Do NOT use for durable preferences (memory.*) or long-horizon Goal findings (context.* / goal.checkpoint). Skip only for trivial one-shot Q&A.",
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
			Name:        "goal.write",
			DisplayName: "Create goal",
			Description: "Create a long-horizon Goal (objective + budgets + pipeline). Use after analysis when work spans multiple segments/runs. Then use todo.write for micro-steps and context.write for findings. Not for short checklists alone.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":            map[string]any{"type": "string"},
					"objective":        map[string]any{"type": "string"},
					"success_criteria": map[string]any{"type": "string"},
					"analysis_summary": map[string]any{"type": "string"},
					"activate":         map[string]any{"type": "boolean"},
				},
				"required": []string{"objective"},
			},
		},
		{
			Name:        "goal.update",
			DisplayName: "Update goal",
			Description: "Update goal fields, pipeline_phase, or action=cancel. Use activate=true to activate a pending goal.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":          map[string]any{"type": "string"},
					"title":            map[string]any{"type": "string"},
					"objective":        map[string]any{"type": "string"},
					"success_criteria": map[string]any{"type": "string"},
					"analysis_summary": map[string]any{"type": "string"},
					"pipeline_phase":   map[string]any{"type": "string"},
					"activate":         map[string]any{"type": "boolean"},
					"action":           map[string]any{"type": "string", "description": "cancel to cancel the goal"},
				},
				"required": []string{"goal_id"},
			},
		},
		{
			Name:        "goal.checkpoint",
			DisplayName: "Goal checkpoint",
			Description: "Save a short progress snapshot on the Goal (status recovery across continues). Prefer one concise summary. Structured findings belong in context.write (kind=finding|decision|handoff); durable user preferences belong in memory.create.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":            map[string]any{"type": "string"},
					"summary":            map[string]any{"type": "string"},
					"progress_note":      map[string]any{"type": "string"},
					"pipeline_phase":     map[string]any{"type": "string"},
					"checkpoint_summary": map[string]any{"type": "string"},
				},
				"required": []string{"summary"},
			},
		},
		{
			Name:        "goal.complete",
			DisplayName: "Complete goal",
			Description: "Mark goal succeeded or failed AFTER final evaluation and user-facing completion report. Requires summary.",
			Risk:        ptools.RiskLow,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal_id":         map[string]any{"type": "string"},
					"status":          map[string]any{"type": "string", "description": "succeeded | failed"},
					"summary":         map[string]any{"type": "string"},
					"report_markdown": map[string]any{"type": "string"},
					"report":          map[string]any{"type": "object"},
				},
				"required": []string{"status", "summary"},
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
			Description: "Append a structured note on the active Goal scratchpad (finding/decision/risk/fact/handoff). Survives segment boundaries and specialist handoffs. Use goal.checkpoint for a short progress snapshot; use memory.create only for lasting preferences across goals.",
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

// slashToolsEnabled gates Temporary Triggers (/read, /shell, 鈥?. Default off
// so plain chat text is never parsed as tools (checklist R6). Enable with
// RED_PANDA_SLASH_TOOLS=1 for local smoke scripts and unit tests.
func slashToolsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_SLASH_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (ToolRunner) Parse(text string, runID string) (ToolInvocation, bool) {
	if !slashToolsEnabled() {
		return ToolInvocation{}, false
	}
	line := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(line, "/tool read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool read "))), true
	case strings.HasPrefix(line, "/read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/read "))), true
	case line == "/tool list" || strings.HasPrefix(line, "/tool list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool list"))), true
	case line == "/list" || strings.HasPrefix(line, "/list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/list"))), true
	case strings.HasPrefix(line, "/tool grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool grep "))), true
	case strings.HasPrefix(line, "/grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/grep "))), true
	case strings.HasPrefix(line, "/tool shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool shell "))), true
	case strings.HasPrefix(line, "/shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/shell "))), true
	case strings.HasPrefix(line, "/tool write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool write "))), true
	case strings.HasPrefix(line, "/write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/write "))), true
	case strings.HasPrefix(line, "/tool edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool edit "))), true
	case strings.HasPrefix(line, "/edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/edit "))), true
	case strings.HasPrefix(line, "/tool diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool diff "))), true
	case strings.HasPrefix(line, "/diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/diff "))), true
	case strings.HasPrefix(line, "/tool patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool patch "))), true
	case strings.HasPrefix(line, "/patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/patch "))), true
	default:
		return ToolInvocation{}, false
	}
}

func (runner ToolRunner) InvocationFromCall(runID string, index int, call ptools.Call, extra ...ptools.Definition) (ToolInvocation, error) {
	if call.Name == "" {
		return ToolInvocation{}, fmt.Errorf("tool name is required")
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool_%s_model_%d", runID, index+1)
	}
	definitions := runner.AvailableTools()
	if len(extra) > 0 {
		definitions = append(append([]ptools.Definition{}, definitions...), extra...)
	}
	for _, definition := range definitions {
		if definition.Name == call.Name {
			if call.DisplayName == "" {
				call.DisplayName = definition.DisplayName
			}
			if call.Risk == "" {
				call.Risk = definition.Risk
			}
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			return ToolInvocation{Call: call}, nil
		}
	}
	// MCP tools may be registered after AvailableTools snapshot; accept prefix as last resort.
	if IsMCPToolName(call.Name) {
		if call.DisplayName == "" {
			call.DisplayName = call.Name
		}
		if call.Risk == "" {
			call.Risk = ptools.RiskHigh
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		return ToolInvocation{Call: call}, nil
	}
	return ToolInvocation{}, fmt.Errorf("unknown tool %s", call.Name)
}

func (runner ToolRunner) Run(ctx context.Context, workingDir string, invocation ToolInvocation) (ptools.Result, string) {
	return runner.RunWithContext(ctx, ToolRunContext{WorkingDir: workingDir}, invocation)
}

func (runner ToolRunner) RunWithContext(ctx context.Context, runCtx ToolRunContext, invocation ToolInvocation) (ptools.Result, string) {
	started := time.Now()
	call := invocation.Call
	result := ptools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     ptools.CallStatusCompleted,
	}

	output, err := runBounded(ctx, toolTimeoutFor(call.Name), func(toolCtx context.Context) (string, error) {
		return runner.dispatchTool(toolCtx, runCtx, call)
	})

	result.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Status = ptools.CallStatusFailed
		result.Error = err.Error()
		// Always emit a standardized envelope (even on failure) so UI/model share one schema.
		result.Output = StandardizeToolOutput(call.Name, output, err, result.DurationMS)
		return result, result.Output
	}
	result.Output = StandardizeToolOutput(call.Name, output, nil, result.DurationMS)
	return result, result.Output
}

// toolTimeoutFor returns the hard upper bound for a tool, or 0 when the tool
// already manages its own deadline (shell/web/subagent/skill).
func toolTimeoutFor(name string) time.Duration {
	switch name {
	case "shell.exec", "web.search", "web.fetch", "skill.run", "subagent.run":
		return 0
	case "memory.list", "memory.create", "memory.update", "memory.delete",
		"todo.write", "todo_write", "todo.list",
		"goal.write", "goal.update", "goal.checkpoint", "goal.complete", "goal.list":
		return defaultGatewayToolTimeout
	default:
		// MCP tools manage start/initialize/call timeouts internally (docs/19).
		if IsMCPToolName(name) {
			return 0
		}
		return defaultLocalToolTimeout
	}
}

// runBounded runs fn and fails fast when timeout elapses so one stuck tool cannot
// freeze the whole agent turn. timeout<=0 means "no outer bound" (self-managed tools).
func runBounded(ctx context.Context, timeout time.Duration, fn func(context.Context) (string, error)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return fn(ctx)
	}

	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type outcome struct {
		output string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := fn(toolCtx)
		done <- outcome{output: out, err: err}
	}()

	select {
	case res := <-done:
		return res.output, res.err
	case <-toolCtx.Done():
		// Prefer a completed result if the tool finished in the same instant as the deadline.
		select {
		case res := <-done:
			return res.output, res.err
		default:
		}
		if err := toolCtx.Err(); err != nil && ctx.Err() != nil {
			return "", err
		}
		return "", fmt.Errorf("tool timed out after %s (operation did not finish; check working_dir, path, or network mounts)", timeout)
	}
}

func (runner ToolRunner) dispatchTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	switch call.Name {
	case "workspace.read_file":
		return runReadFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"))
	case "workspace.list":
		return runListWorkspace(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", defaultListDepth))
	case "workspace.stats":
		return runWorkspaceStats(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", 4))
	case "workspace.grep":
		return runGrepWorkspace(runCtx.WorkingDir, StringArg(call.Arguments, "pattern"), StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_matches", defaultGrepMatches))
	case "workspace.write_file":
		return runWriteFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "content"))
	case "workspace.edit_file":
		return runEditFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
	case "workspace.diff_file":
		content, hasContent := stringArgPresent(call.Arguments, "content")
		return runDiffFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), content, hasContent, StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
	case "workspace.apply_patch":
		return runApplyPatch(runCtx.WorkingDir, StringArg(call.Arguments, "patch"))
	case "shell.exec":
		return runShell(ctx, runCtx.WorkingDir, StringArg(call.Arguments, "command"))
	case "skill.list":
		return skill.RunList(runCtx.WorkingDir)
	case "skill.create":
		return skill.RunCreate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
	case "skill.update":
		return skill.RunUpdate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
	case "skill.delete":
		return skill.RunDelete(runCtx.WorkingDir, StringArg(call.Arguments, "name"))
	case "skill.run":
		if runner.SkillExecutor == nil {
			return "", fmt.Errorf("skill subagent executor is not available")
		}
		return runner.SkillExecutor(ctx, runCtx, call)
	case "subagent.run":
		if runner.SubagentExecutor == nil {
			return "", fmt.Errorf("subagent executor is not available")
		}
		return runner.SubagentExecutor(ctx, runCtx, call)
	case "subagent.list":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.List(runCtx, call)
	case "subagent.cancel":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.Cancel(runCtx, call)
	case "subagent.reset":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.Reset(runCtx, call)
	case "subagent.pool_status":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.PoolStatus()
	case "subagent.pool_resize":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.PoolResize(call)
	case "subagent.pool_reset":
		if runner.SubagentManager == nil {
			return "", fmt.Errorf("subagent manager is not available")
		}
		return runner.SubagentManager.PoolReset()
	case "todo.write", "todo_write", "todo.list":
		return runner.runTodoTool(ctx, runCtx, call)
	case "goal.write", "goal.update", "goal.checkpoint", "goal.complete", "goal.list":
		return runner.runGoalTool(ctx, runCtx, call)
	case "context.read", "context.search", "context.write", "context.replace", "context.delete":
		return runner.runContextTool(ctx, runCtx, call)
	case "memory.list", "memory.create", "memory.update", "memory.delete":
		return runner.runMemoryTool(ctx, runCtx, call)
	case "web.search":
		searchOpts := effectiveWebSearchOptions(runCtx)
		return runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebSearch(
				opCtx,
				StringArg(call.Arguments, "query"),
				effectiveWebResultCount(runCtx, IntArg(call.Arguments, "max_results", 0)),
				searchOpts,
			)
		})
	case "web.fetch":
		return runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebFetch(
				opCtx,
				StringArg(call.Arguments, "url"),
				effectiveWebFetchBytes(runCtx, IntArg(call.Arguments, "max_bytes", 0)),
				effectiveWebHTTPProxy(runCtx),
			)
		})
	default:
		if IsMCPToolName(call.Name) {
			if runner.MCPExecutor == nil {
				return "", fmt.Errorf("MCP executor is not available")
			}
			return runner.MCPExecutor(ctx, runCtx, call)
		}
		return "", fmt.Errorf("unknown tool %s", call.Name)
	}
}

func TruncateToolOutput(value string) string {
	if len(value) <= maxToolOutputBytes {
		return value
	}
	return value[:maxToolOutputBytes] + "\n[truncated]"
}
func clampInt(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func StringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func StringArgDefault(args map[string]any, key string, fallback string) string {
	value := strings.TrimSpace(StringArg(args, key))
	if value == "" {
		return fallback
	}
	return value
}

func IntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		if parsed, err := value.Int64(); err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringArgPresent(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	return value, ok
}

type jsonNumber interface {
	Int64() (int64, error)
}
