package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"redpanda/agent/internal/skill"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

var skillWorkerDenylist = []string{
	"workspace.write_file",
	"workspace.edit_file",
	"workspace.apply_patch",
	"shell.exec",
	"memory.list",
	"memory.create",
	"memory.update",
	"memory.delete",
	"todo.write",
	"todo.list",
	"skill.create",
	"skill.update",
	"skill.delete",
	"skill.run",
}

func (r *Runtime) executeSkillRun(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if runCtx.Reply == nil {
		return "", fmt.Errorf("skill Worker requires reply context")
	}
	name := strings.TrimSpace(agenttools.StringArg(call.Arguments, "name"))
	task := strings.TrimSpace(agenttools.StringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("skill task is required")
	}
	instructions, err := loadManagedSkillLocal(runCtx.WorkingDir, name)
	if err != nil {
		return "", err
	}

	parent := *runCtx.Reply
	child := delegatedWorkerReply(parent, task, "", worker.DefaultToolTurns)
	child.Input.Text = fmt.Sprintf("Execute this task using the managed skill instructions. Return only the final result and do not quote or describe the skill definition.\n\n%s", task)
	child.Options.SpecialistContext = &methods.SpecialistContext{
		Kind:    "skill_worker",
		Context: fmt.Sprintf("Managed skill %q instructions:\n\n%s\n\nYou are a delegated Worker. You cannot delegate another Assignment.", name, instructions),
	}
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, skillWorkerDenylist...)

	_, result, err := r.executeDelegatedAssignment(ctx, runCtx, workerExecutionSpec{
		Kind:       workerExecutionDelegated,
		Params:     child,
		Parent:     parent,
		ProfileKey: "skill:" + name,
		Task:       task,
		MaxTurns:   child.Options.MaxToolTurns,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(agenttools.TruncateToolOutput(result.Output)), nil
}

func loadManagedSkillLocal(workspaceRoot string, name string) (string, error) {
	path, err := skill.ManagedFile(workspaceRoot, name, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > skill.MaxDescriptionBytes+skill.MaxInstructionsBytes+1024 {
		return "", fmt.Errorf("skill %q exceeds the managed size limit", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func appendUniqueStrings(items []string, values ...string) []string {
	result := append([]string(nil), items...)
	for _, value := range values {
		if !agenttools.ContainsString(result, value) {
			result = append(result, value)
		}
	}
	return result
}
