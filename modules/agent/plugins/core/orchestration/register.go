package orchestration

import (
	"context"
	"fmt"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	orchestration "redpanda/agent/plugins/core/orchestration/internal"
	ptools "redpanda/protocol/tools"
)

// source prefix for orchestration tools.
const source = "builtin:orchestration"

// toolSpec holds the registration parameters without committing to a
// specific Handler type; the Handler is assigned after wrapping.
type toolSpec struct {
	def          ptools.Definition
	timeoutClass registry.TimeoutClass
	opsOnly      bool
	handler      orchestration.HandlerFunc
	hostExecutor HostExecutor
}

type HostExecutor func(context.Context, *registry.ToolContext, ptools.Call) (string, error)

// Dependencies are host capabilities required by orchestration tools. The
// plugin owns tool registration; Runtime only supplies implementations that
// need access to the Worker pool or run lifecycle.
type Dependencies struct {
	SkillRun         HostExecutor
	WorkerDelegate   HostExecutor
	WorkerList       HostExecutor
	WorkerResult     HostExecutor
	WorkerCancel     HostExecutor
	WorkerPoolStatus HostExecutor
	WorkerSend       HostExecutor
	WorkerReceive    HostExecutor
}

// Register registers the skill.* and worker.* tools into reg.
// Order of registration is preserved; returned names reflect that order.
func Register(reg *registry.Registry, bus *hooks.ExtensionBus, deps Dependencies) []string {
	specs := []toolSpec{
		{
			def: ptools.Definition{
				Name:        "skill.list",
				DisplayName: "List skills",
				Description: "List managed workspace skills under .codex/skills with name and description only. The catalog is also injected at conversation start; call this to re-check after skill.create/update/delete in the same run.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      false,
			handler:      orchestration.HandlerSkillList,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.GatewayToolTimeout,
			opsOnly:      true,
			handler:      orchestration.HandlerSkillCreate,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.GatewayToolTimeout,
			opsOnly:      true,
			handler:      orchestration.HandlerSkillUpdate,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.GatewayToolTimeout,
			opsOnly:      true,
			handler:      orchestration.HandlerSkillDelete,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.SelfManagedToolTimeout,
			opsOnly:      false,
			hostExecutor: deps.SkillRun,
		},
		{
			def: ptools.Definition{
				Name:        "worker.delegate",
				DisplayName: "Delegate work",
				Description: "Delegate one focused task to an available Worker and return its assignment identity and final report. Delegated assignments cannot delegate again.",
				Risk:        ptools.RiskMedium,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task":        map[string]any{"type": "string", "description": "Focused task with scope and expected output."},
						"profile_key": map[string]any{"type": "string", "description": "Optional Worker Profile key."},
						"max_turns":   map[string]any{"type": "integer", "minimum": 1, "description": "Optional model/tool-loop budget. Each loop is one provider request and may request multiple tools; one extra text-only final synthesis request can occur. Executed tools are separately bounded."},
						"file_count":  map[string]any{"type": "integer", "minimum": 1, "description": "Files in this scope. When max_turns is omitted, the runtime derives a generous budget from this count."},
						"path":        map[string]any{"type": "string", "description": "Assigned workspace scope for diagnostics."},
					},
					"required": []string{"task"},
				},
			},
			timeoutClass: registry.SelfManagedToolTimeout,
			opsOnly:      false,
			hostExecutor: deps.WorkerDelegate,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      false,
			hostExecutor: deps.WorkerList,
		},
		{
			def: ptools.Definition{
				Name:        "worker.result",
				DisplayName: "Read Worker report",
				Description: "Read a completed Worker's report by Assignment ID. Use offset/next_offset to continue an oversized report; do not create another Assignment merely because report transport was truncated.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"assignment_id": map[string]any{"type": "string", "description": "Completed Assignment ID returned by worker.delegate."},
						"offset":        map[string]any{"type": "integer", "minimum": 0, "description": "Byte offset, normally the previous next_offset."},
						"max_bytes":     map[string]any{"type": "integer", "minimum": 1, "maximum": 24576, "description": "Maximum report bytes for this chunk."},
					},
					"required": []string{"assignment_id"},
				},
			},
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      false,
			hostExecutor: deps.WorkerResult,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      false,
			hostExecutor: deps.WorkerCancel,
		},
		{
			def: ptools.Definition{
				Name:        "worker.pool_status",
				DisplayName: "Worker pool status",
				Description: "Inspect Worker capacity, health, and active Assignment counts.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      true,
			hostExecutor: deps.WorkerPoolStatus,
		},
		{
			def: ptools.Definition{
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
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      true,
			hostExecutor: deps.WorkerSend,
		},
		{
			def: ptools.Definition{
				Name:        "worker.receive",
				DisplayName: "Receive Worker message",
				Description: "Wait for the next message addressed to the current active Assignment.",
				Risk:        ptools.RiskLow,
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			timeoutClass: registry.LocalToolTimeout,
			opsOnly:      true,
			hostExecutor: deps.WorkerReceive,
		},
	}

	names := make([]string, 0, len(specs))
	for _, s := range specs {
		var h registry.HandlerFunc
		if s.hostExecutor != nil {
			execute := s.hostExecutor
			name := s.def.Name
			h = func(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				output, err := execute(ctx, toolCtx, ptools.Call{ID: toolCtx.ToolCallID, Name: name, Arguments: args})
				result := &ptools.Result{Name: name, Status: ptools.CallStatusCompleted, Output: output}
				if err != nil {
					result.Status = ptools.CallStatusFailed
					result.Error = err.Error()
				}
				return result, err
			}
		} else {
			inner := s.handler
			h = func(ctx context.Context, toolCtx *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
				if inner == nil {
					return nil, fmt.Errorf("host capability for %s is unavailable", s.def.Name)
				}
				ictx := &orchestration.ToolContext{
					RunID: toolCtx.RunID, SessionID: toolCtx.SessionID,
					AssignmentID: toolCtx.AssignmentID, WorkerID: toolCtx.WorkerID,
					WorkingDir: toolCtx.WorkingDir, Reply: toolCtx.Reply,
				}
				ires, err := inner(ctx, ictx, args)
				if err != nil {
					return &ptools.Result{Status: ptools.CallStatusFailed, Error: err.Error()}, nil
				}
				return &ptools.Result{Status: ptools.CallStatusCompleted, Output: ires.Output}, nil
			}
		}

		entry := registry.ToolEntry{
			Definition:   s.def,
			Handler:      h,
			TimeoutClass: s.timeoutClass,
			OpsOnly:      s.opsOnly,
			Source:       source,
		}
		reg.MustRegister(entry)
		names = append(names, s.def.Name)
	}

	_ = bus // reserved for future hook-based registration
	return names
}
