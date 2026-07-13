package runtime

import (
	"context"
	"fmt"
	"os"
	"redpanda/agent/internal/skill"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

var skillSubagentDenylist = []string{
	"workspace.write_file",
	"workspace.edit_file",
	"workspace.apply_patch",
	"shell.exec",
	"memory.list",
	"memory.create",
	"memory.update",
	"memory.delete",
	"todo.write",
	"goal.write",
	"goal.update",
	"goal.checkpoint",
	"goal.complete",
	"goal.list",
	"todo.list",
	"todo_write",
	"skill.create",
	"skill.update",
	"skill.delete",
	"skill.run",
}

func (r *Runtime) executeSkillRun(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if runCtx.Reply == nil {
		return "", fmt.Errorf("skill subagent requires reply context")
	}
	name := strings.TrimSpace(agenttools.StringArg(call.Arguments, "name"))
	task := strings.TrimSpace(agenttools.StringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("skill task is required")
	}
	skill, err := loadManagedSkillLocal(runCtx.WorkingDir, name)
	if err != nil {
		return "", err
	}

	params := *runCtx.Reply
	subAgentID := "skill_" + name + "_" + params.RunID
	agentName := "skill:" + name
	childRunID := params.RunID + ":subagent:" + subAgentID
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.subagents.Register(subagent.Registration{
		SubAgentID:      subAgentID,
		Name:            agentName,
		Backend:         "runtime_process",
		RootRunID:       params.RunID,
		ParentRunID:     params.RunID,
		ParentSessionID: params.Session.ID,
		ChildRunID:      childRunID,
		Summary:         agentName + " subagent started",
		Cancel:          cancel,
	})
	_ = r.emitAgentEvent(ctx, params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"skill_name":  name,
		"status":      "running",
		"summary":     "isolated skill subagent started",
		"backend":     "runtime_process",
	})

	child, err := r.newProcessSubAgent(childCtx, params, subAgentID)
	if err != nil {
		r.failSkillSubAgent(params, subAgentID, agentName, name, err)
		return "", err
	}
	defer child.Close(context.Background())

	childParams := params
	childParams.RunID = childRunID
	childParams.Session.Conversation = nil
	childParams.Input.Text = fmt.Sprintf("Execute this task using the managed skill instructions. Return only the final result and do not quote or describe the skill definition.\n\n%s", task)
	// 技能正文是角色和指令，不属于长期记忆。
	childParams.Options.MemoryContext = nil
	childParams.Options.SpecialistContext = &methods.SpecialistContext{
		Kind:    "skill",
		Context: fmt.Sprintf("Managed skill %q instructions:\n\n%s", name, skill),
	}
	childParams.Options.TodoContext = nil
	disableGoalPipelineForChild(&childParams.Options)
	childParams.Options.RequirePermission = false
	childParams.Options.SpawnSubAgents = false
	childParams.Options.SubAgentBackend = ""
	childParams.Options.ToolDenylist = appendUniqueStrings(childParams.Options.ToolDenylist, skillSubagentDenylist...)

	var output strings.Builder
	childStatus := ""
	bridgeSpec := subagent.RunSpec{
		RootRunID:  params.RunID,
		SubAgentID: subAgentID,
		Name:       agentName,
		Backend:    "runtime_process",
		Parent:     params,
		Child:      childParams,
	}
	err = child.Start(childCtx, childParams, func(event events.Envelope) {
		if event.Type == events.EventMessageDelta && event.Agent.Role == events.AgentRoleRoot {
			if delta, ok := event.Payload["delta"].(string); ok {
				output.WriteString(delta)
			}
		}
		if event.Type == events.EventFinish {
			childStatus, _ = event.Payload["status"].(string)
		}
		_ = (runtimeEventSink{runtime: r}).Bridge(context.Background(), bridgeSpec, event)
	})
	if err != nil {
		if childCtx.Err() != nil {
			r.subagents.Finish(params.RunID, subAgentID, "cancelled", "isolated skill subagent cancelled", childCtx.Err().Error())
			return "", childCtx.Err()
		}
		r.failSkillSubAgent(params, subAgentID, agentName, name, err)
		return "", err
	}
	if childStatus != "" && childStatus != "completed" {
		err := fmt.Errorf("skill subagent finished with status %s", childStatus)
		r.failSkillSubAgent(params, subAgentID, agentName, name, err)
		return "", err
	}
	result := strings.TrimSpace(agenttools.TruncateToolOutput(output.String()))
	if result == "" {
		err := fmt.Errorf("skill subagent returned an empty result")
		r.failSkillSubAgent(params, subAgentID, agentName, name, err)
		return "", err
	}
	r.subagents.Finish(params.RunID, subAgentID, "completed", "isolated skill subagent completed", "")
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"skill_name":  name,
		"status":      "completed",
		"summary":     "isolated skill subagent completed",
		"backend":     "runtime_process",
	})
	return result, nil
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

func (r *Runtime) failSkillSubAgent(params methods.ReplyParams, subAgentID string, agentName string, skillName string, err error) {
	r.subagents.Finish(params.RunID, subAgentID, "failed", "isolated skill subagent failed", err.Error())
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"skill_name":  skillName,
		"status":      "failed",
		"summary":     "isolated skill subagent failed",
		"backend":     "runtime_process",
		"error":       err.Error(),
	})
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
