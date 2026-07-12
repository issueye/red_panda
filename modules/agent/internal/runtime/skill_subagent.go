package runtime

import (
	"context"
	"fmt"
	"os"
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

func (r *Runtime) executeSkillRun(ctx context.Context, runCtx ToolRunContext, call tools.Call) (string, error) {
	if runCtx.Reply == nil {
		return "", fmt.Errorf("skill subagent requires reply context")
	}
	name := strings.TrimSpace(stringArg(call.Arguments, "name"))
	task := strings.TrimSpace(stringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("skill task is required")
	}
	skill, err := loadManagedSkill(runCtx.WorkingDir, name)
	if err != nil {
		return "", err
	}

	params := *runCtx.Reply
	subAgentID := "skill_" + name + "_" + params.RunID
	agentName := "skill:" + name
	childRunID := params.RunID + ":subagent:" + subAgentID
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.registerSubAgent(params, subAgentID, agentName, "runtime_process", cancel)
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
	childParams.Options.MemoryContext = &methods.MemoryContext{
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
	err = child.Start(childCtx, childParams, func(event events.Envelope) {
		if event.Type == events.EventMessageDelta && event.Agent.Role == events.AgentRoleRoot {
			if delta, ok := event.Payload["delta"].(string); ok {
				output.WriteString(delta)
			}
		}
		if event.Type == events.EventFinish {
			childStatus, _ = event.Payload["status"].(string)
		}
		r.bridgeProcessSubAgentEvent(context.Background(), params, subAgentID, agentName, "runtime_process", event)
	})
	if err != nil {
		if childCtx.Err() != nil {
			r.finishSubAgent(params.RunID, subAgentID, "cancelled", "isolated skill subagent cancelled", childCtx.Err().Error())
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
	result := strings.TrimSpace(truncateToolOutput(output.String()))
	if result == "" {
		err := fmt.Errorf("skill subagent returned an empty result")
		r.failSkillSubAgent(params, subAgentID, agentName, name, err)
		return "", err
	}
	r.finishSubAgent(params.RunID, subAgentID, "completed", "isolated skill subagent completed", "")
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

func loadManagedSkill(workspaceRoot string, name string) (string, error) {
	path, err := managedSkillFile(workspaceRoot, name, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxSkillDescriptionBytes+maxSkillInstructionsBytes+1024 {
		return "", fmt.Errorf("skill %q exceeds the managed size limit", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (r *Runtime) failSkillSubAgent(params methods.ReplyParams, subAgentID string, agentName string, skillName string, err error) {
	r.finishSubAgent(params.RunID, subAgentID, "failed", "isolated skill subagent failed", err.Error())
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
		if !containsString(result, value) {
			result = append(result, value)
		}
	}
	return result
}
