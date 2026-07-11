package runtime

import (
	"encoding/json"
	"strings"

	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func (r *Runtime) handleAgentSkills(req jsonrpc.Request) error {
	var params methods.SkillsListParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	items, err := listManagedSkills(params.WorkspaceRoot)
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, err.Error()))
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.SkillsListResult{Items: items})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleAgentSkillLoad(req jsonrpc.Request) error {
	var params methods.SkillLoadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if strings.TrimSpace(params.Name) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "skill name is required"))
	}
	include := params.IncludeInstructions
	// Default management loads to include instructions when omitted is false;
	// callers must opt in. Desktop/Gateway should pass include_instructions=true.
	detail, err := loadManagedSkillDetail(params.WorkspaceRoot, params.Name, include)
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, err.Error()))
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.SkillLoadResult{Skill: detail})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) handleAgentSkillCreate(req jsonrpc.Request) error {
	var params methods.SkillMutateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	output, err := runCreateSkill(params.WorkspaceRoot, params.Name, params.Description, params.Instructions)
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, err.Error()))
	}
	return r.writeSkillMutateResult(req, "skill.create", params.Name, output)
}

func (r *Runtime) handleAgentSkillUpdate(req jsonrpc.Request) error {
	var params methods.SkillMutateParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	output, err := runUpdateSkill(params.WorkspaceRoot, params.Name, params.Description, params.Instructions)
	if err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, err.Error()))
	}
	return r.writeSkillMutateResult(req, "skill.update", params.Name, output)
}

func (r *Runtime) handleAgentSkillDelete(req jsonrpc.Request) error {
	var params methods.SkillDeleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "invalid params"))
	}
	if strings.TrimSpace(params.Name) == "" {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32602, "skill name is required"))
	}
	if _, err := runDeleteSkill(params.WorkspaceRoot, params.Name); err != nil {
		return r.writeResponse(jsonrpc.NewError(req.ID, -32000, err.Error()))
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.SkillDeleteResult{
		Action:  "skill.delete",
		Name:    strings.TrimSpace(params.Name),
		Deleted: true,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}

func (r *Runtime) writeSkillMutateResult(req jsonrpc.Request, action string, name string, toolOutput string) error {
	path := ""
	var parsed map[string]string
	if err := json.Unmarshal([]byte(toolOutput), &parsed); err == nil {
		path = parsed["path"]
		if parsed["name"] != "" {
			name = parsed["name"]
		}
		if parsed["action"] != "" {
			action = parsed["action"]
		}
	}
	resp, err := jsonrpc.NewResult(req.ID, methods.SkillMutateResult{
		Action: action,
		Name:   name,
		Path:   path,
	})
	if err != nil {
		return err
	}
	return r.writeResponse(resp)
}
